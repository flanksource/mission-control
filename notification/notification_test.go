package notification_test

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	dbModels "github.com/flanksource/incident-commander/db/models"
	"github.com/flanksource/incident-commander/events"
	"github.com/google/uuid"
	"github.com/lib/pq"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	// register event handlers
	_ "github.com/flanksource/incident-commander/incidents/responder"
	_ "github.com/flanksource/incident-commander/notification"
	_ "github.com/flanksource/incident-commander/playbook"
	_ "github.com/flanksource/incident-commander/upstream"
)

var _ = ginkgo.Describe("Notification on incident creation", ginkgo.Ordered, func() {
	var (
		john      *models.Person
		incident  *models.Incident
		component *models.Component
		team      *dbModels.Team
	)

	ginkgo.It("should create a person", func() {
		john = &models.Person{
			ID:   uuid.New(),
			Name: "James Bond",
		}
		tx := DefaultContext.DB().Create(john)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create a new component", func() {
		component = &models.Component{
			ID:         uuid.New(),
			Name:       "logistics",
			Type:       "Entity",
			ExternalId: "dummy/logistics",
		}
		tx := DefaultContext.DB().Create(component)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create a team", func() {
		teamSpec := api.TeamSpec{
			Components: []types.ResourceSelector{{Name: "logistics"}},
			Notifications: []api.NotificationConfig{
				{
					URL: fmt.Sprintf("generic+%s", webhookEndpoint),
					Properties: map[string]string{
						"template":   "json",
						"disabletls": "yes",
						"title":      "New incident: {{.incident.title}}",
					},
				},
			},
		}

		specRaw, err := collections.StructToJSON(teamSpec)
		Expect(err).To(BeNil())

		var spec types.JSONMap
		err = json.Unmarshal([]byte(specRaw), &spec)
		Expect(err).To(BeNil())

		team = &dbModels.Team{
			ID:        uuid.New(),
			Name:      "ghostbusters",
			CreatedBy: john.ID,
			Spec:      spec,
		}
		tx := DefaultContext.DB().Create(team)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create a new notification", func() {
		notif := models.Notification{
			ID:       uuid.New(),
			Events:   pq.StringArray([]string{"incident.created"}),
			Template: "Severity: {{.incident.severity}}",
			TeamID:   &team.ID,
			Source:   models.SourceCRD,
		}

		err := DefaultContext.DB().Create(&notif).Error
		Expect(err).To(BeNil())
	})

	ginkgo.It("should create an incident", func() {
		incident = &models.Incident{
			ID:          uuid.New(),
			Title:       "Constantly hitting threshold",
			CreatedBy:   john.ID,
			Type:        models.IncidentTypeCost,
			Status:      models.IncidentStatusOpen,
			Severity:    "Blocker",
			CommanderID: &john.ID,
		}
		tx := DefaultContext.DB().Create(incident)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should consume the event and send the notification", func() {
		events.ConsumeAll(DefaultContext)

		Eventually(func() int {
			return len(webhookPostdata)
		}, "10s", "200ms").Should(BeNumerically(">=", 1))

		Expect(webhookPostdata).To(Not(BeNil()))
		Expect(webhookPostdata["message"]).To(Equal(fmt.Sprintf("Severity: %s", incident.Severity)))
		Expect(webhookPostdata["title"]).To(Equal(fmt.Sprintf("New incident: %s", incident.Title)))
	})

	ginkgo.It("should create a new notification with repeat interval", func() {
		n := models.Notification{
			ID:             uuid.New(),
			Events:         pq.StringArray([]string{"config.created"}),
			Template:       "Config created",
			TeamID:         &team.ID,
			Source:         models.SourceCRD,
			RepeatInterval: "4h",
			GroupBy:        pq.StringArray([]string{"notification_id", "source_event"}),
		}
		err := DefaultContext.DB().Create(&n).Error
		Expect(err).To(BeNil())

		config1 := &models.ConfigItem{
			ID:          uuid.New(),
			Name:        lo.ToPtr("Deployment1"),
			ConfigClass: models.ConfigClassDeployment,
			Type:        lo.ToPtr("Kubernetes::Deployment"),
		}
		err = DefaultContext.DB().Create(config1).Error
		Expect(err).To(BeNil())

		events.ConsumeAll(DefaultContext)
		Eventually(func() int64 {
			var c int64
			DefaultContext.DB().Model(&models.Event{}).Count(&c)
			return c
		}, "10s", "200ms").Should(Equal(int64(0)))

		// Check send history
		var sentHistoryCount int64
		err = DefaultContext.DB().Model(&models.NotificationSendHistory{}).Where("notification_id = ?", n.ID).Count(&sentHistoryCount).Error
		Expect(err).To(BeNil())
		Expect(sentHistoryCount).To(Equal(int64(1)))

		config2 := &models.ConfigItem{
			ID:          uuid.New(),
			Name:        lo.ToPtr("Deployment2"),
			ConfigClass: models.ConfigClassDeployment,
			Type:        lo.ToPtr("Kubernetes::Deployment"),
		}
		err = DefaultContext.DB().Create(config2).Error
		Expect(err).To(BeNil())

		events.ConsumeAll(DefaultContext)
		Eventually(func() int64 {
			var c int64
			DefaultContext.DB().Model(&models.Event{}).Count(&c)
			return c
		}, "10s", "200ms").Should(Equal(int64(0)))

		// Check send history
		err = DefaultContext.DB().Model(&models.NotificationSendHistory{}).Where("notification_id = ?", n.ID).Count(&sentHistoryCount).Error
		Expect(err).To(BeNil())
		Expect(sentHistoryCount).To(Equal(int64(1)))

		err = DefaultContext.DB().Model(&models.NotificationSendHistory{}).Where("notification_id = ?", n.ID).Update("created_at", time.Now().Add(-10*time.Hour)).Error
		Expect(err).To(BeNil())

		config3 := &models.ConfigItem{
			ID:          uuid.New(),
			Name:        lo.ToPtr("Deployment3"),
			ConfigClass: models.ConfigClassDeployment,
			Type:        lo.ToPtr("Kubernetes::Deployment"),
		}
		err = DefaultContext.DB().Create(config3).Error
		Expect(err).To(BeNil())

		events.ConsumeAll(DefaultContext)
		Eventually(func() int64 {
			var c int64
			DefaultContext.DB().Model(&models.Event{}).Count(&c)
			return c
		}, "10s", "200ms").Should(Equal(int64(0)))

		// Check send history
		err = DefaultContext.DB().Model(&models.NotificationSendHistory{}).Where("notification_id = ?", n.ID).Count(&sentHistoryCount).Error
		Expect(err).To(BeNil())
		Expect(sentHistoryCount).To(Equal(int64(2)))
	})
})
