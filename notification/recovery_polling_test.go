//go:build recoverytests

package notification

import (
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func recoveryProperties(values map[string]string) context.Context {
	annotations := map[string]string{}
	for k, v := range values {
		annotations["mission-control/notification.recovery."+k] = v
	}
	return setup.DefaultContext.WithObject(metav1.ObjectMeta{Name: uuid.NewString(), Annotations: annotations})
}

func pollingReceipt(n NotificationWithSpec, payload NotificationEventPayload) models.NotificationDelivery {
	now := time.Now().Add(-time.Hour)
	receipt := models.NotificationDelivery{ID: uuid.New(), NotificationID: n.ID, EpisodeID: uuid.MustParse(payload.RecoveryEpisode), Transport: "smtp", Destination: types.JSON(`{}`), Policy: types.JSON(`{"enabled":true,"waitFor":"1h2m3.5s"}`), MessageID: uuid.NewString(), Status: "waiting-for-healthy", SentAt: &now, NotBefore: now}
	Expect(setup.DefaultContext.DB().Create(&receipt).Error).To(Succeed())
	return receipt
}

func readPollingReceipt(id uuid.UUID) models.NotificationDelivery {
	var receipt models.NotificationDelivery
	Expect(setup.DefaultContext.DB().Where("id = ?", id).First(&receipt).Error).To(Succeed())
	return receipt
}

var _ = ginkgo.Describe("Notification recovery polling", func() {
	ginkgo.It("post-send wake queues only its delivery without draining unrelated ready work", func() {
		n, config, payload := newRecoveryFixture()
		other, _, otherPayload := newRecoveryFixture()
		unrelated := pollingReceipt(other, otherPayload)
		Expect(setup.DefaultContext.DB().Model(&unrelated).Update("status", "recovery-error").Error).To(Succeed())
		before := readPollingReceipt(unrelated.ID)
		ctx := recoveryContext(&n, payload)
		receipt, err := beginDelivery(ctx, "smtp", recoveryDestination{To: []string{"local@example.com"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		Expect(finishDelivery(ctx, receipt, "", "", nil)).To(Succeed())
		actual := readPollingReceipt(receipt.ID)
		Expect(actual.Status).To(Equal("stabilizing"))
		Expect(actual.Attempts).To(BeZero())
		Expect(actual.ReplyAt).To(BeNil())
		Expect(readPollingReceipt(unrelated.ID)).To(Equal(before))
	})

	ginkgo.It("bounds the number of marked resources per sweep", func() {
		n, config, payload := newRecoveryFixture()
		other, otherConfig, otherPayload := newRecoveryFixture()
		pollingReceipt(n, payload)
		pollingReceipt(other, otherPayload)
		tx := setup.DefaultContext.DB().Begin()
		defer tx.Rollback()
		ctx := recoveryProperties(map[string]string{"sweep.batch-size": "1"}).WithDB(tx, nil)
		Expect(tx.Exec("UPDATE notification_health_states SET wake_pending=false").Error).To(Succeed())
		Expect(tx.Model(&config).Update("health", "healthy").Error).To(Succeed())
		Expect(tx.Model(&otherConfig).Update("health", "healthy").Error).To(Succeed())
		Expect(SweepNotificationRecoveries(ctx)).To(Succeed())
		var count int64
		Expect(tx.Model(&models.NotificationDelivery{}).Where("notification_id IN ? AND status='stabilizing'", []uuid.UUID{n.ID, other.ID}).Count(&count).Error).To(Succeed())
		Expect(count).To(Equal(int64(1)))
		Expect(SweepNotificationRecoveries(ctx)).To(Succeed())
		Expect(tx.Model(&models.NotificationDelivery{}).Where("notification_id IN ? AND status='stabilizing'", []uuid.UUID{n.ID, other.ID}).Count(&count).Error).To(Succeed())
		Expect(count).To(Equal(int64(2)))
	})

	ginkgo.It("excludes dormant rows from ready claims without any writes", func() {
		n, _, payload := newRecoveryFixture()
		receipt := pollingReceipt(n, payload)
		before := readPollingReceipt(receipt.ID)
		for range 3 {
			Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		}
		Expect(readPollingReceipt(receipt.ID)).To(Equal(before))
	})

	ginkgo.It("targets one resource and the exact saved Go duration without a global drain", func() {
		n, config, payload := newRecoveryFixture()
		other, otherConfig, otherPayload := newRecoveryFixture()
		receipt, unrelated := pollingReceipt(n, payload), pollingReceipt(other, otherPayload)
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&otherConfig).Update("health", "healthy").Error).To(Succeed())
		before := readPollingReceipt(unrelated.ID)
		handler := notificationHandler{}
		Expect(handler.addNotificationEvent(setup.DefaultContext, models.Event{Name: "config.healthy", EventID: config.ID})).To(Succeed())
		actual := readPollingReceipt(receipt.ID)
		state, err := currentRecoveryHealth(setup.DefaultContext, "config", config.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual.Status).To(Equal("stabilizing"))
		Expect(actual.NotBefore).To(BeTemporally("==", state.HealthySince.Add(time.Hour+2*time.Minute+3500*time.Millisecond)))
		Expect(actual.Attempts).To(BeZero())
		Expect(actual.ReplyAt).To(BeNil())
		Expect(readPollingReceipt(unrelated.ID)).To(Equal(before))
	})

	ginkgo.It("bounds resource wake batches and sweeps missed/coalesced transitions", func() {
		n, config, payload := newRecoveryFixture()
		receipts := []models.NotificationDelivery{pollingReceipt(n, payload), pollingReceipt(n, payload), pollingReceipt(n, payload)}
		ctx := recoveryProperties(map[string]string{"wake.batch-size": "1"})
		for _, health := range []string{"healthy", "unhealthy", "healthy"} {
			Expect(ctx.DB().Model(&config).Update("health", health).Error).To(Succeed())
		}
		for i := 1; i <= 3; i++ {
			Expect(SweepNotificationRecoveries(ctx)).To(Succeed())
			var count int64
			Expect(ctx.DB().Model(&models.NotificationDelivery{}).Where("notification_id = ? AND status = 'stabilizing'", n.ID).Count(&count).Error).To(Succeed())
			Expect(count).To(Equal(int64(i)))
		}
		state, err := currentRecoveryHealth(ctx, "config", config.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(state.WakePending).To(BeFalse())
		for _, receipt := range receipts {
			Expect(readPollingReceipt(receipt.ID).Attempts).To(BeZero())
		}
	})

	ginkgo.It("preserves failure backoff, exhaustion, completed operations and active leases", func() {
		n, config, payload := newRecoveryFixture()
		deadline := time.Now().Add(3 * time.Hour).Truncate(time.Microsecond)
		for _, status := range []string{"waiting-for-healthy", "recovery-error", "recovery-exhausted", "leased"} {
			receipt := pollingReceipt(n, payload)
			values := map[string]any{"attempts": 5, "not_before": deadline, "reply_at": time.Now(), "reaction_at": time.Now()}
			if status == "leased" {
				values["lease_until"], values["lease_token"] = deadline, uuid.New()
			} else {
				values["status"] = status
			}
			Expect(setup.DefaultContext.DB().Model(&receipt).Updates(values).Error).To(Succeed())
		}
		before := loadRecoveryReceipts(n)
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		Expect(wakeNotificationResource(setup.DefaultContext, "config", config.ID)).To(Succeed())
		Expect(SweepNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		after := loadRecoveryReceipts(n)
		before[0].Status = "stabilizing"
		Expect(after).To(Equal(before))
	})

	ginkgo.It("does not replenish accumulated failure budget on flapping health deferrals", func() {
		n, config, payload := newRecoveryFixture()
		receipt := pollingReceipt(n, payload)
		Expect(setup.DefaultContext.DB().Model(&receipt).Updates(map[string]any{"attempts": 4, "status": "recovery-error"}).Error).To(Succeed())
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		dormant := readPollingReceipt(receipt.ID)
		Expect(dormant.Status).To(Equal("waiting-for-healthy"))
		Expect(dormant.Attempts).To(Equal(4))
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		Expect(wakeNotificationResource(setup.DefaultContext, "config", config.ID)).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "unhealthy").Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&receipt).Update("not_before", time.Now().Add(-time.Second)).Error).To(Succeed())
		Expect(ReconcileNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		Expect(readPollingReceipt(receipt.ID).Attempts).To(Equal(4))
		Expect(readPollingReceipt(receipt.ID).Status).To(Equal("waiting-for-healthy"))
	})

	ginkgo.It("rearms a healthy wake racing the final persistence of an unhealthy deferral", func() {
		n, config, payload := newRecoveryFixture()
		receipt := pollingReceipt(n, payload)
		token := uuid.New()
		Expect(setup.DefaultContext.DB().Model(&receipt).Updates(map[string]any{"status": "stabilizing", "lease_token": token, "lease_until": time.Now().Add(time.Minute)}).Error).To(Succeed())
		Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
		Expect(wakeNotificationResource(setup.DefaultContext, "config", config.ID)).To(Succeed())
		state, err := currentRecoveryHealth(setup.DefaultContext, "config", config.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(state.WakePending).To(BeFalse())
		Expect(finishRecoveryClaim(setup.DefaultContext, receipt, token, map[string]any{"status": "waiting-for-healthy", "lease_until": nil, "lease_token": nil})).To(Succeed())
		Expect(SweepNotificationRecoveries(setup.DefaultContext)).To(Succeed())
		Expect(readPollingReceipt(receipt.ID).Status).To(Equal("stabilizing"))
		Expect(finishRecoveryClaim(setup.DefaultContext, receipt, token, map[string]any{"status": "resolved"})).To(HaveOccurred())
	})

	for _, clearFirst := range []bool{true, false} {
		name := "blocks dormant completion behind an uncommitted marker clear"
		if !clearFirst {
			name = "blocks marker clearing behind an uncommitted dormant completion"
		}
		ginkgo.It(name, func() {
			n, config, payload := newRecoveryFixture()
			receipt := pollingReceipt(n, payload)
			token := uuid.New()
			Expect(setup.DefaultContext.DB().Model(&receipt).Updates(map[string]any{"status": "stabilizing", "lease_token": token, "lease_until": time.Now().Add(time.Minute)}).Error).To(Succeed())
			Expect(setup.DefaultContext.DB().Model(&config).Update("health", "healthy").Error).To(Succeed())
			first := setup.DefaultContext.DB().Begin()
			Expect(first.Error).NotTo(HaveOccurred())
			defer first.Rollback()
			second := setup.DefaultContext.DB().Begin()
			Expect(second.Error).NotTo(HaveOccurred())
			defer func() {
				first.Rollback()
				second.Rollback()
			}()
			var firstPID, secondPID int64
			Expect(first.Raw("SELECT pg_backend_pid()").Scan(&firstPID).Error).To(Succeed())
			Expect(second.Raw("SELECT pg_backend_pid()").Scan(&secondPID).Error).To(Succeed())
			complete := func(ctx context.Context) error {
				return finishRecoveryClaim(ctx, receipt, token, map[string]any{"status": "waiting-for-healthy", "lease_until": nil, "lease_token": nil})
			}
			clear := func(ctx context.Context) error {
				// Refresh has already observed committed healthy state; test the batch's own lock protocol.
				return wakeNotificationResourceBatch(ctx, "config", config.ID)
			}
			firstOp, secondOp := clear, complete
			if !clearFirst {
				firstOp, secondOp = complete, clear
			}
			Expect(firstOp(setup.DefaultContext.WithDB(first, nil))).To(Succeed())
			done := make(chan error, 1)
			go func() { done <- secondOp(setup.DefaultContext.WithDB(second, nil)) }()
			Eventually(func() []int64 {
				var blockers []int64
				Expect(setup.DefaultContext.DB().Raw("SELECT unnest(pg_blocking_pids(?))::bigint", secondPID).Scan(&blockers).Error).To(Succeed())
				return blockers
			}, "2s").Should(ContainElement(firstPID))
			Expect(first.Commit().Error).To(Succeed())
			Eventually(done, "2s").Should(Receive(BeNil()))
			Expect(second.Commit().Error).To(Succeed())
			if clearFirst {
				Expect(readPollingReceipt(receipt.ID).Status).To(Equal("waiting-for-healthy"))
				var pending bool
				Expect(setup.DefaultContext.DB().Model(&models.NotificationHealthState{}).Select("wake_pending").Where("resource_type = 'config' AND resource_id = ?", config.ID).Scan(&pending).Error).To(Succeed())
				Expect(pending).To(BeTrue())
			}
			Expect(SweepNotificationRecoveries(setup.DefaultContext)).To(Succeed())
			actual := readPollingReceipt(receipt.ID)
			Expect(actual.Status).To(Equal("stabilizing"))
			Expect(actual.Attempts).To(BeZero())
		})
	}

	ginkgo.It("revalidates stale healthy unlogged check markers before waking after restart loss", func() {
		n, _, payload := newRecoveryFixture()
		tx := setup.DefaultContext.DB().Begin()
		defer tx.Rollback()
		ctx := setup.DefaultContext.WithDB(tx, nil)
		checkID := uuid.UUID(dummy.LogisticsAPIHealthHTTPCheck.ID)
		Expect(tx.Exec("DELETE FROM checks_unlogged WHERE check_id = ?", checkID).Error).To(Succeed())
		Expect(tx.Exec("SELECT record_notification_health('check', ?, 'unhealthy')", checkID).Error).To(Succeed())
		var state models.NotificationHealthState
		Expect(tx.Where("resource_type = 'check' AND resource_id = ?", checkID).First(&state).Error).To(Succeed())
		now := time.Now()
		receipt := models.NotificationDelivery{ID: uuid.New(), NotificationID: n.ID, EpisodeID: *state.EpisodeID, Transport: "smtp", Destination: types.JSON(`{}`), Policy: types.JSON(`{}`), MessageID: payload.RecoveryEpisode, Status: "waiting-for-healthy", SentAt: &now, NotBefore: time.Now()}
		Expect(tx.Create(&receipt).Error).To(Succeed())
		Expect(tx.Exec("SELECT record_notification_health('check', ?, 'healthy')", checkID).Error).To(Succeed())
		Expect(SweepNotificationRecoveries(ctx)).To(Succeed())
		latest, err := currentRecoveryHealth(ctx, "check", checkID)
		Expect(err).NotTo(HaveOccurred())
		Expect(latest.Health).To(Equal("unknown"))
		Expect(latest.WakePending).To(BeFalse())
		var actual models.NotificationDelivery
		Expect(tx.Where("id = ?", receipt.ID).First(&actual).Error).To(Succeed())
		Expect(actual.Status).To(Equal("waiting-for-healthy"))
		Expect(actual.Attempts).To(BeZero())
	})
})
