package notification_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	dbModels "github.com/flanksource/incident-commander/db/models"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/notification"
	"github.com/google/uuid"
	"github.com/lib/pq"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	// register event handlers
	_ "github.com/flanksource/incident-commander/incidents/responder"
	_ "github.com/flanksource/incident-commander/playbook"
	_ "github.com/flanksource/incident-commander/upstream"
)

var _ = ginkgo.Describe("Notifications", ginkgo.Ordered, func() {
	var customReceiverJson []byte

	ginkgo.BeforeAll(func() {
		customReceiver := []api.NotificationConfig{
			{
				URL: fmt.Sprintf("generic+%s", webhookEndpoint),
				Properties: map[string]string{
					"disabletls": "yes",
					"template":   "json",
				},
			},
		}
		var err error
		customReceiverJson, err = json.Marshal(customReceiver)
		Expect(err).To(BeNil())
	})

	var _ = ginkgo.Describe("Notification on incident creation", ginkgo.Ordered, func() {
		var (
			notif     models.Notification
			john      *models.Person
			incident  *models.Incident
			component *models.Component
			team      *dbModels.Team
		)

		ginkgo.AfterAll(func() {
			err := DefaultContext.DB().Delete(&notif).Error
			Expect(err).To(BeNil())

			notification.PurgeCache(notif.ID.String())
		})

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
			notif = models.Notification{
				ID:       uuid.New(),
				Name:     "incident-test-notification",
				Events:   pq.StringArray([]string{"incident.created"}),
				Template: "Severity: {{.incident.severity}}",
				TeamID:   &team.ID,
				Source:   models.SourceCRD,
				Filter:   fmt.Sprintf("incident.type == '%s'", models.IncidentTypeCost),
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
	})

	var _ = ginkgo.Describe("repeat interval test", ginkgo.Ordered, func() {
		var n models.Notification
		var config models.ConfigItem

		ginkgo.BeforeAll(func() {
			n = models.Notification{
				ID:             uuid.New(),
				Name:           "repeat-interval-test",
				Events:         pq.StringArray([]string{"config.updated"}),
				Source:         models.SourceCRD,
				Title:          "Dummy",
				Template:       "dummy",
				CustomServices: types.JSON(customReceiverJson),
				RepeatInterval: "4h",
			}

			err := DefaultContext.DB().Create(&n).Error
			Expect(err).To(BeNil())

			config = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("Deployment1"),
				ConfigClass: models.ConfigClassDeployment,
				Config:      lo.ToPtr(`{"color": "red"}`),
				Type:        lo.ToPtr("Kubernetes::Deployment"),
			}

			err = DefaultContext.DB().Create(&config).Error
			Expect(err).To(BeNil())
		})

		ginkgo.AfterAll(func() {
			err := DefaultContext.DB().Delete(&n).Error
			Expect(err).To(BeNil())

			err = DefaultContext.DB().Delete(&config).Error
			Expect(err).To(BeNil())

			notification.PurgeCache(n.ID.String())
		})

		ginkgo.It("should have sent a notification for a config update", func() {
			event := models.Event{
				Name:       "config.updated",
				Properties: types.JSONStringMap{"id": config.ID.String()},
			}
			err := DefaultContext.DB().Create(&event).Error
			Expect(err).To(BeNil())

			events.ConsumeAll(DefaultContext)
			Eventually(func() int64 {
				var c int64
				DefaultContext.DB().Model(&models.Event{}).Where("name = 'config.updated'").Count(&c)
				return c
			}, "10s", "200ms").Should(Equal(int64(0)))

			Eventually(func() int64 {
				// Check send history
				var sentHistoryCount int64
				err = DefaultContext.DB().Model(&models.NotificationSendHistory{}).Where("notification_id = ?", n.ID).Count(&sentHistoryCount).Error
				Expect(err).To(BeNil())
				return sentHistoryCount
			}, "10s", "200ms").Should(Equal(int64(1)))
		})

		ginkgo.It("should NOT have sent a notification for a subsequent config update", func() {
			event := models.Event{
				Name:       "config.updated",
				Properties: types.JSONStringMap{"id": config.ID.String()},
			}
			err := DefaultContext.DB().Create(&event).Error
			Expect(err).To(BeNil())

			events.ConsumeAll(DefaultContext)
			Eventually(func() int64 {
				var c int64
				DefaultContext.DB().Model(&models.Event{}).Where("name = 'config.updated'").Count(&c)
				return c
			}, "10s", "200ms").Should(Equal(int64(0)))

			// Check send history
			var sentHistoryCount int64
			err = DefaultContext.DB().Model(&models.NotificationSendHistory{}).
				Where("notification_id = ?", n.ID).
				Where("status = ?", models.NotificationStatusRepeatInterval).
				Count(&sentHistoryCount).Error
			Expect(err).To(BeNil())
			Expect(sentHistoryCount).To(Equal(int64(1)))
		})
	})

	var _ = ginkgo.Describe("notification error handling on send", ginkgo.Ordered, func() {
		var goodNotif models.Notification
		var badNotif models.Notification
		var deployment1 models.ConfigItem
		var pod1 models.ConfigItem

		ginkgo.BeforeAll(func() {
			{
				goodNotif = models.Notification{
					ID:             uuid.New(),
					Name:           "test-notification-error-on-send-1",
					Events:         pq.StringArray([]string{"config.updated"}),
					Filter:         ".config.type == 'Kubernetes::Deployment'",
					Source:         models.SourceCRD,
					Title:          "Dummy",
					Template:       "dummy",
					CustomServices: types.JSON(customReceiverJson),
				}

				err := DefaultContext.DB().Create(&goodNotif).Error
				Expect(err).To(BeNil())
			}

			{
				badReceiver := []api.NotificationConfig{
					{
						URL: "generic+bad",
						Properties: map[string]string{
							"disabletls": "yes",
							"template":   "json",
						},
					},
				}
				customReceiverJson, err := json.Marshal(badReceiver)
				Expect(err).To(BeNil())

				badNotif = models.Notification{
					ID:             uuid.New(),
					Name:           "test-notification-error-on-send-2",
					Events:         pq.StringArray([]string{"config.updated"}),
					Filter:         ".config.type == 'Kubernetes::Pod'",
					Source:         models.SourceCRD,
					Title:          "Dummy",
					Template:       "dummy",
					CustomServices: types.JSON(customReceiverJson),
				}

				err = DefaultContext.DB().Create(&badNotif).Error
				Expect(err).To(BeNil())
			}

			{
				deployment1 = models.ConfigItem{
					ID:          uuid.New(),
					Name:        lo.ToPtr("deployment-1"),
					ConfigClass: models.ConfigClassDeployment,
					Config:      lo.ToPtr(`{"replicas": 1}`),
					Type:        lo.ToPtr("Kubernetes::Deployment"),
				}

				err := DefaultContext.DB().Create(&deployment1).Error
				Expect(err).To(BeNil())
			}

			{
				pod1 = models.ConfigItem{
					ID:          uuid.New(),
					Name:        lo.ToPtr("deployment-2"),
					ConfigClass: models.ConfigClassDeployment,
					Config:      lo.ToPtr(`{"replicas": 2}`),
					Type:        lo.ToPtr("Kubernetes::Pod"),
				}

				err := DefaultContext.DB().Create(&pod1).Error
				Expect(err).To(BeNil())
			}
		})

		ginkgo.AfterAll(func() {
			err := DefaultContext.DB().Delete(&goodNotif).Error
			Expect(err).To(BeNil())
			err = DefaultContext.DB().Delete(&badNotif).Error
			Expect(err).To(BeNil())
			err = DefaultContext.DB().Delete(&deployment1).Error
			Expect(err).To(BeNil())
			err = DefaultContext.DB().Delete(&pod1).Error
			Expect(err).To(BeNil())

			notification.PurgeCache(goodNotif.ID.String())
			notification.PurgeCache(badNotif.ID.String())
		})

		ginkgo.It("should have consumed all events", func() {
			testEvents := []models.Event{
				{
					Name:       "config.updated",
					Properties: types.JSONStringMap{"id": deployment1.ID.String()},
				}, {
					Name:       "config.updated",
					Properties: types.JSONStringMap{"id": pod1.ID.String()},
				},
			}
			err := DefaultContext.DB().Create(&testEvents).Error
			Expect(err).To(BeNil())

			events.ConsumeAll(DefaultContext)
			Eventually(func() int64 {
				var c int64
				DefaultContext.DB().Model(&models.Event{}).Where("name = 'config.updated'").Count(&c)
				return c
			}, "10s", "200ms").Should(Equal(int64(0)))
		})

		ginkgo.It("one notification.send event with max attempt should be in the event_queue", func() {
			Eventually(func() int {
				var event models.Event
				err := DefaultContext.DB().Where("name = 'notification.send'").First(&event).Error
				Expect(err).To(BeNil())
				return event.Attempts
			}, "10s", "200ms").Should(Equal(4))

			var event models.Event
			err := DefaultContext.DB().Where("name = 'notification.send'").First(&event).Error
			Expect(err).To(BeNil())
			Expect(event.Priority).To(Equal(-4))
		})

		ginkgo.It("only one notification must have been sent", func() {
			var sentHistory []models.NotificationSendHistory
			err := DefaultContext.DB().Where("notification_id = ?", goodNotif.ID).Find(&sentHistory).Error
			Expect(err).To(BeNil())
			Expect(len(sentHistory)).To(Equal(1))
			Expect(sentHistory[0].Status).To(Equal(models.NotificationStatusSent))
		})

		ginkgo.It("should correctly set error status", func() {
			var sentHistory models.NotificationSendHistory
			err := DefaultContext.DB().Where("notification_id = ?", badNotif.ID).Find(&sentHistory).Error
			Expect(err).To(BeNil())
			Expect(sentHistory.Status).To(Equal(models.NotificationStatusError))
		})
	})

	var _ = ginkgo.Describe("notification wait for", ginkgo.Ordered, func() {
		var n models.Notification
		var config models.ConfigItem

		ginkgo.BeforeAll(func() {
			n = models.Notification{
				ID:             uuid.New(),
				Name:           "wait-for-test",
				Events:         pq.StringArray([]string{"config.healthy", "config.unhealthy"}),
				Source:         models.SourceCRD,
				Title:          "Dummy",
				Template:       "dummy",
				CustomServices: types.JSON(customReceiverJson),
				WaitFor:        lo.ToPtr(time.Second * 5),
			}

			err := DefaultContext.DB().Create(&n).Error
			Expect(err).To(BeNil())

			config = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("navidrome"),
				ConfigClass: models.ConfigClassDeployment,
				Health:      lo.ToPtr(models.HealthHealthy),
				Config:      lo.ToPtr(`{"color": "red"}`),
				Type:        lo.ToPtr("Kubernetes::Deployment"),
			}

			err = DefaultContext.DB().Create(&config).Error
			Expect(err).To(BeNil())

			events.ConsumeAll(DefaultContext)
		})

		ginkgo.AfterAll(func() {
			err := DefaultContext.DB().Delete(&n).Error
			Expect(err).To(BeNil())

			err = DefaultContext.DB().Delete(&config).Error
			Expect(err).To(BeNil())

			notification.PurgeCache(n.ID.String())
		})

		ginkgo.It("should create a new pending send history", func() {
			err := DefaultContext.DB().Model(&models.ConfigItem{}).Where("id = ?", config.ID).Update("health", models.HealthUnhealthy).Error
			Expect(err).To(BeNil())

			events.ConsumeAll(DefaultContext)

			Eventually(func() bool {
				var history models.NotificationSendHistory
				err = DefaultContext.DB().Where("notification_id = ?", n.ID.String()).Where("status = ?", models.NotificationStatusPending).Find(&history).Error
				Expect(err).To(BeNil())

				return history.ID != uuid.Nil
			}, "5s", "1s").Should(BeTrue())
		})

		ginkgo.It("should not consume the event within the delay period", func() {
			for i := 0; i < 5; i++ {
				_, err := notification.ProcessPendingNotification(DefaultContext)
				Expect(err).To(BeNil())

				var history models.NotificationSendHistory
				err = DefaultContext.DB().Where("notification_id = ?", n.ID.String()).Where("status = ?", models.NotificationStatusPending).First(&history).Error
				Expect(err).To(BeNil())
			}
		})

		ginkgo.It("it should eventually consume the event", func() {
			Eventually(func() bool {
				DefaultContext.Logger.V(0).Infof("checking if the delayed notification.send event has been consumed")

				_, err := notification.ProcessPendingNotification(DefaultContext)
				Expect(err).To(BeNil())

				var pending []models.NotificationSendHistory
				err = DefaultContext.DB().Where("notification_id = ?", n.ID.String()).Where("status = ?", models.NotificationStatusPending).Find(&pending).Error
				Expect(err).To(BeNil())
				return len(pending) == 0
			}, "15s", "1s").Should(BeTrue())
		})
	})

	var _ = ginkgo.Describe("template vailidity", func() {
		for _, event := range api.EventStatusGroup {
			ginkgo.It(event, func() {
				title, body := notification.DefaultTitleAndBody(event)
				msg := notification.NotificationTemplate{
					Message: body,
					Title:   title,
				}

				originalEvent := models.Event{
					Name:       event,
					Properties: map[string]string{},
				}

				switch {
				case strings.HasPrefix(event, "config"):
					originalEvent.Properties["id"] = dummy.EKSCluster.ID.String()
				case strings.HasPrefix(event, "check"):
					var latestCheckStatus models.CheckStatus
					err := DefaultContext.DB().Where("check_id = ?", dummy.LogisticsAPIHealthHTTPCheck.ID).First(&latestCheckStatus).Error
					Expect(err).To(BeNil())

					originalEvent.Properties["id"] = dummy.LogisticsAPIHealthHTTPCheck.ID.String()
					originalEvent.Properties["last_runtime"] = latestCheckStatus.Time

				case strings.HasPrefix(event, "component"):
					originalEvent.Properties["id"] = dummy.Logistics.ID.String()
				}

				celEnv, err := notification.GetEnvForEvent(DefaultContext, originalEvent)
				Expect(err).To(BeNil())

				celEnv.Channel = "slack"
				templater := DefaultContext.NewStructTemplater(celEnv.AsMap(), "", notification.TemplateFuncs)
				err = templater.Walk(&msg)
				Expect(err).To(BeNil())

				var slackBlock map[string]any
				err = json.Unmarshal([]byte(msg.Message), &slackBlock)
				Expect(err).To(BeNil())
			})
		}
	})
})
