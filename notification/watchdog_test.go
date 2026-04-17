package notification_test

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/lib/pq"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/robfig/cron/v3"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/notification"
)

func createWatchdogNotification() *models.Notification {
	customServices, err := json.Marshal([]api.NotificationConfig{{
		URL: fmt.Sprintf("generic+%s", webhookEndpoint),
		Properties: map[string]string{
			"disabletls": "yes",
			"template":   "json",
		},
	}})
	Expect(err).To(BeNil())

	notif := &models.Notification{
		ID:             uuid.New(),
		Name:           fmt.Sprintf("watchdog-%s", uuid.NewString()),
		Namespace:      "default",
		Events:         pq.StringArray([]string{api.EventConfigUnhealthy}),
		Source:         models.SourceCRD,
		CustomServices: types.JSON(customServices),
	}
	Expect(DefaultContext.DB().Create(notif).Error).To(BeNil())

	ginkgo.DeferCleanup(func() {
		Expect(DefaultContext.DB().Where("notification_id = ?", notif.ID).Delete(&models.NotificationSendHistory{}).Error).To(BeNil())
		Expect(DefaultContext.DB().Where("id = ?", notif.ID).Delete(&models.Notification{}).Error).To(BeNil())
		notification.PurgeCache(notif.ID.String())
	})

	return notif
}

var _ = ginkgo.Describe("Notification watchdog", func() {
	ginkgo.It("should unschedule watchdog jobs when a notification is deleted", func() {
		scheduler := cron.New()
		ginkgo.DeferCleanup(func() {
			scheduler.Stop()
		})

		notificationID := uuid.New().String()
		schedule := "*/1 * * * *"
		Expect(notification.SyncWatchdogJob(DefaultContext, scheduler, notificationID, &schedule)).To(BeNil())
		Expect(scheduler.Entries()).To(HaveLen(1))

		Expect(notification.SyncWatchdogJob(DefaultContext, scheduler, notificationID, nil)).To(BeNil())
		Expect(scheduler.Entries()).To(BeEmpty())
	})

	ginkgo.It("should treat an empty watchdog schedule as disabled", func() {
		scheduler := cron.New()
		ginkgo.DeferCleanup(func() {
			scheduler.Stop()
		})

		notificationID := uuid.New().String()
		schedule := "*/1 * * * *"
		Expect(notification.SyncWatchdogJob(DefaultContext, scheduler, notificationID, &schedule)).To(BeNil())
		Expect(scheduler.Entries()).To(HaveLen(1))

		disabled := "   "
		Expect(notification.SyncWatchdogJob(DefaultContext, scheduler, notificationID, &disabled)).To(BeNil())
		Expect(scheduler.Entries()).To(BeEmpty())
	})

	ginkgo.It("should keep the existing watchdog when schedule update fails", func() {
		scheduler := cron.New()
		ginkgo.DeferCleanup(func() {
			scheduler.Stop()
		})

		notificationID := uuid.New().String()
		schedule := "*/1 * * * *"
		Expect(notification.SyncWatchdogJob(DefaultContext, scheduler, notificationID, &schedule)).To(BeNil())
		Expect(scheduler.Entries()).To(HaveLen(1))

		invalidSchedule := "not-a-cron-expression"
		Expect(notification.SyncWatchdogJob(DefaultContext, scheduler, notificationID, &invalidSchedule)).To(HaveOccurred())
		Expect(scheduler.Entries()).To(HaveLen(1))
	})

	ginkgo.It("should resolve watchdog statistics from the event id", func() {
		notif := createWatchdogNotification()

		env, err := notification.GetEnvForEvent(DefaultContext, models.Event{
			Name:    api.EventWatchdog,
			EventID: notif.ID,
		})
		Expect(err).To(BeNil())
		Expect(env.Summary.ID).To(Equal(notif.ID.String()))
		Expect(env.Summary.Name).To(Equal(notif.Name))
	})

	ginkgo.It("should send a watchdog notification without requiring a fallback recipient", func() {
		notif := createWatchdogNotification()
		webhookPostdata = nil

		err := notification.SendWatchdogNotification(DefaultContext, notif.ID.String())
		Expect(err).To(BeNil())

		Eventually(func() int {
			return len(webhookPostdata)
		}, "5s", "100ms").Should(BeNumerically(">", 0))

		var count int64
		Expect(DefaultContext.DB().Model(&models.NotificationSendHistory{}).
			Where("notification_id = ?", notif.ID).
			Where("source_event = ?", api.EventWatchdog).
			Count(&count).Error).To(BeNil())
		Expect(count).To(Equal(int64(1)))
	})
})
