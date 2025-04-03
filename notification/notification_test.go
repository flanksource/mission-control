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
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
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

	var _ = ginkgo.Describe("playbook recipient", ginkgo.Ordered, func() {
		var myNotification models.Notification
		var playbook models.Playbook
		var config models.ConfigItem
		var sendHistory models.NotificationSendHistory

		ginkgo.BeforeAll(func() {
			playbookSpec := v1.PlaybookSpec{
				Actions: []v1.PlaybookAction{
					{
						Name: "just echo",
						Exec: &v1.ExecAction{
							Script: `echo "{{.config.name}} {{.config.id}}"`,
						},
					},
				},
			}
			specRaw, err := json.Marshal(playbookSpec)
			Expect(err).To(BeNil())

			playbook = models.Playbook{
				Source: models.SourceCRD,
				Spec:   specRaw,
			}

			err = DefaultContext.DB().Create(&playbook).Error
			Expect(err).To(BeNil())

			myNotification = models.Notification{
				ID:             uuid.New(),
				Name:           "playbook",
				Events:         pq.StringArray([]string{"config.updated"}),
				Source:         models.SourceCRD,
				Title:          "Dummy",
				Template:       "dummy",
				PlaybookID:     &playbook.ID,
				RepeatInterval: "4h",
			}

			err = DefaultContext.DB().Create(&myNotification).Error
			Expect(err).To(BeNil())

			config = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("Deployment2"),
				ConfigClass: models.ConfigClassDeployment,
				Config:      lo.ToPtr(`{"color": "red"}`),
				Type:        lo.ToPtr("Kubernetes::Deployment"),
			}

			err = DefaultContext.DB().Create(&config).Error
			Expect(err).To(BeNil())
		})

		ginkgo.AfterAll(func() {
			if sendHistory.PlaybookRunID != nil {
				err := DefaultContext.DB().Exec("DELETE FROM playbook_run_actions WHERE playbook_run_id = ?", *sendHistory.PlaybookRunID).Error
				Expect(err).To(BeNil())
			}

			err := DefaultContext.DB().Delete(&playbook).Error
			Expect(err).To(BeNil())

			err = DefaultContext.DB().Delete(&myNotification).Error
			Expect(err).To(BeNil())

			err = DefaultContext.DB().Delete(&config).Error
			Expect(err).To(BeNil())

			notification.PurgeCache(myNotification.ID.String())
		})

		ginkgo.It("should have created a notification with a pending playbook run status for a config update", func() {
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
			}, "10s", "200ms").Should(Equal(int64(0)), "must have consumed the config.updated event")

			Eventually(func() bool {
				DefaultContext.DB().Where("source_event = ?", api.EventConfigUpdated).
					Where("resource_id = ?", config.ID.String()).
					Where("notification_id = ?", myNotification.ID.String()).
					Where("playbook_run_id IS NOT NULL").
					Where("status IN ?", []string{
						models.NotificationStatusPendingPlaybookRun,
						models.NotificationStatusPendingPlaybookCompletion,
						models.NotificationStatusSent,
						models.NotificationStatusError,
					}).Find(&sendHistory)
				return sendHistory.ID != uuid.Nil
			}, "10s", "200ms").Should(BeTrue(), "must have created a notification with playbook run attached")
		})

		ginkgo.It("should have created a playbook run", func() {
			Eventually(func() models.PlaybookRunStatus {
				var playbookRun models.PlaybookRun
				Expect(DefaultContext.DB().Where("id = ?", *sendHistory.PlaybookRunID).First(&playbookRun).Error).To(BeNil())

				return playbookRun.Status
			}, "10s", "200ms").Should(Equal(models.PlaybookRunStatusCompleted), "the recipient playbook must have completed successfully")
		})

		ginkgo.It("the playbook should have correct data", func() {
			var playbookRunActions []models.PlaybookRunAction
			Expect(DefaultContext.DB().Where("playbook_run_id = ?", *sendHistory.PlaybookRunID).Find(&playbookRunActions).Error).To(BeNil())

			Expect(len(playbookRunActions)).To(Equal(1))

			stdout := playbookRunActions[0].Result["stdout"]
			Expect(stdout).To(Equal(fmt.Sprintf("%s %s", *config.Name, config.ID.String())))
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

	var _ = ginkgo.Describe("inhibitions", ginkgo.Ordered, func() {
		var n models.Notification
		var deployment, pod, replicaSet models.ConfigItem

		inhibitions := []v1.NotificationInihibition{
			{
				Direction: "incoming",
				From:      "Kubernetes::Pod",
				To: []string{
					"Kubernetes::Deployment",
					"Kubernetes::ReplicaSet",
				},
			},
		}

		inhibitionsJSON, err := json.Marshal(inhibitions)
		Expect(err).To(BeNil())

		ginkgo.BeforeAll(func() {
			n = models.Notification{
				ID:             uuid.New(),
				Name:           "inhibition-test",
				Events:         pq.StringArray([]string{"config.unhealthy"}),
				Source:         models.SourceCRD,
				Title:          "Dummy",
				Template:       "dummy",
				CustomServices: types.JSON(customReceiverJson),
				Inhibitions:    inhibitionsJSON,
				RepeatInterval: "4h",
			}

			err := DefaultContext.DB().Create(&n).Error
			Expect(err).To(BeNil())

			deployment = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("airsonic"),
				ConfigClass: models.ConfigClassDeployment,
				Config:      lo.ToPtr(`{"color": "red"}`),
				Labels: &types.JSONStringMap{
					"app": "airsonic",
				},
				Type: lo.ToPtr("Kubernetes::Deployment"),
			}

			err = DefaultContext.DB().Create(&deployment).Error
			Expect(err).To(BeNil())

			replicaSet = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("airsonic-replicaset"),
				ConfigClass: "ReplicaSet",
				ParentID:    &deployment.ID,
				Config:      lo.ToPtr(`{"replicas": 1}`),
				Labels: &types.JSONStringMap{
					"app": "airsonic",
				},
				Type: lo.ToPtr("Kubernetes::ReplicaSet"),
			}

			err = DefaultContext.DB().Create(&replicaSet).Error
			Expect(err).To(BeNil())

			pod = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("airsonic-pod"),
				ConfigClass: models.ConfigClassPod,
				ParentID:    &replicaSet.ID,
				Config:      lo.ToPtr(`{"color": "blue"}`),
				Labels: &types.JSONStringMap{
					"app": "airsonic",
				},
				Type: lo.ToPtr("Kubernetes::Pod"),
			}

			err = DefaultContext.DB().Create(&pod).Error
			Expect(err).To(BeNil())
		})

		ginkgo.AfterAll(func() {
			err := DefaultContext.DB().Delete(&n).Error
			Expect(err).To(BeNil())

			err = DefaultContext.DB().Delete(&pod).Error
			Expect(err).To(BeNil())

			err = DefaultContext.DB().Delete(&replicaSet).Error
			Expect(err).To(BeNil())

			err = DefaultContext.DB().Delete(&deployment).Error
			Expect(err).To(BeNil())

			notification.PurgeCache(n.ID.String())
		})

		ginkgo.It("should have sent a notification for a config update", func() {
			event := models.Event{
				Name:       "config.unhealthy",
				Properties: types.JSONStringMap{"id": pod.ID.String()},
			}
			err := DefaultContext.DB().Create(&event).Error
			Expect(err).To(BeNil())

			events.ConsumeAll(DefaultContext)
			Eventually(func() int64 {
				var c int64
				DefaultContext.DB().Model(&models.Event{}).Where("name = 'config.unhealthy'").Count(&c)
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

		ginkgo.It("should NOT have sent a notification for a subsequent replica set unhealthy", func() {
			event := models.Event{
				Name:       "config.unhealthy",
				Properties: types.JSONStringMap{"id": replicaSet.ID.String()},
			}
			err := DefaultContext.DB().Create(&event).Error
			Expect(err).To(BeNil())

			events.ConsumeAll(DefaultContext)
			Eventually(func() int64 {
				events.ConsumeAll(DefaultContext)

				var c int64
				DefaultContext.DB().Model(&models.Event{}).Where("name = 'config.unhealthy'").Count(&c)
				return c
			}, "20s", "200ms").Should(Equal(int64(0)))

			// Check send history
			var histories []models.NotificationSendHistory
			err = DefaultContext.DB().Where("notification_id = ?", n.ID).Find(&histories).Error
			Expect(err).To(BeNil())
			Expect(len(histories)).To(Equal(2))

			for _, history := range histories {
				if history.ResourceID == replicaSet.ID {
					Expect(history.Status).To(Equal(models.NotificationStatusInhibited))
					Expect(history.ParentID).To(Not(BeNil()))
				}
				if history.ResourceID == pod.ID {
					Expect(history.Status).To(Equal(models.NotificationStatusSent))
				}
			}
		})

		ginkgo.It("should NOT have sent a notification for a subsequent deployment update", func() {
			event := models.Event{
				Name:       "config.unhealthy",
				Properties: types.JSONStringMap{"id": deployment.ID.String()},
			}
			err := DefaultContext.DB().Create(&event).Error
			Expect(err).To(BeNil())

			events.ConsumeAll(DefaultContext)
			Eventually(func() int64 {
				events.ConsumeAll(DefaultContext)

				var c int64
				DefaultContext.DB().Model(&models.Event{}).Where("name = 'config.unhealthy'").Count(&c)
				return c
			}, "20s", "200ms").Should(Equal(int64(0)))

			// Check send history
			var histories []models.NotificationSendHistory
			err = DefaultContext.DB().Where("notification_id = ?", n.ID).Find(&histories).Error
			Expect(err).To(BeNil())
			Expect(len(histories)).To(Equal(3))

			for _, history := range histories {
				if history.ResourceID == replicaSet.ID {
					Expect(history.Status).To(Equal(models.NotificationStatusInhibited))
					Expect(history.ParentID).To(Not(BeNil()))
				}
				if history.ResourceID == deployment.ID {
					Expect(history.Status).To(Equal(models.NotificationStatusInhibited))
					Expect(history.ParentID).To(Not(BeNil()))
				}
				if history.ResourceID == pod.ID {
					Expect(history.Status).To(Equal(models.NotificationStatusSent))
				}
			}
		})

		ginkgo.It("should NOT have sent a notification for a another replica set unhealthy", func() {
			event := models.Event{
				Name:       "config.unhealthy",
				Properties: types.JSONStringMap{"id": replicaSet.ID.String()},
			}
			err := DefaultContext.DB().Create(&event).Error
			Expect(err).To(BeNil())

			events.ConsumeAll(DefaultContext)
			Eventually(func() int64 {
				events.ConsumeAll(DefaultContext)

				var c int64
				DefaultContext.DB().Model(&models.Event{}).Where("name = 'config.unhealthy'").Count(&c)
				return c
			}, "20s", "200ms").Should(Equal(int64(0)))

			// Check send history
			var histories []models.NotificationSendHistory
			err = DefaultContext.DB().Where("notification_id = ?", n.ID).Find(&histories).Error
			Expect(err).To(BeNil())
			Expect(len(histories)).To(Equal(3))

			for _, history := range histories {
				if history.ResourceID == replicaSet.ID {
					Expect(history.Status).To(Equal(models.NotificationStatusInhibited))
					Expect(history.ParentID).To(Not(BeNil()))
					Expect(history.Count).To(Equal(2))
				}
				if history.ResourceID == deployment.ID {
					Expect(history.Status).To(Equal(models.NotificationStatusInhibited))
					Expect(history.ParentID).To(Not(BeNil()))
				}
				if history.ResourceID == pod.ID {
					Expect(history.Status).To(Equal(models.NotificationStatusSent))
				}
			}
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
				Events:         pq.StringArray([]string{"config.healthy", "config.warning", "config.unhealthy"}),
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
				_, err := notification.ProcessPendingNotifications(DefaultContext)
				Expect(err).To(BeNil())

				var history models.NotificationSendHistory
				err = DefaultContext.DB().Where("notification_id = ?", n.ID.String()).Where("status = ?", models.NotificationStatusPending).First(&history).Error
				Expect(err).To(BeNil())
			}
		})

		ginkgo.It("it should eventually consume the event", func() {
			Eventually(func() bool {
				DefaultContext.Logger.V(0).Infof("checking if the delayed notification.send event has been consumed")

				_, err := notification.ProcessPendingNotifications(DefaultContext)
				Expect(err).To(BeNil())

				var pending []models.NotificationSendHistory
				err = DefaultContext.DB().Where("notification_id = ?", n.ID.String()).Where("status = ?", models.NotificationStatusPending).Find(&pending).Error
				Expect(err).To(BeNil())
				return len(pending) == 0
			}, "15s", "1s").Should(BeTrue())
		})

		ginkgo.It("should have sent out a notification", func() {
			var sendHistory []models.NotificationSendHistory
			err := DefaultContext.DB().
				Where("notification_id = ?", n.ID.String()).
				Where("resource_id = ?", config.ID.String()).
				Where("source_event = ?", "config.unhealthy").
				Find(&sendHistory).Error
			Expect(err).To(BeNil())
			Expect(len(sendHistory)).To(Equal(1))
		})

		ginkgo.It("`should not send out a notification`", func() {
			{
				// Change health to warning & then back to unknown
				// This should create 1 notification.send event for the 'warning' health.
				// since the health is changed immediately, we shouldn't receive a notification for it.
				err := DefaultContext.DB().Model(&models.ConfigItem{}).Where("id = ?", config.ID).Update("health", models.HealthWarning).Error
				Expect(err).To(BeNil())

				err = DefaultContext.DB().Model(&models.ConfigItem{}).Where("id = ?", config.ID).Update("health", models.HealthUnknown).Error
				Expect(err).To(BeNil())
			}

			Eventually(func() bool {
				events.ConsumeAll(DefaultContext)

				var pending []models.NotificationSendHistory
				err := DefaultContext.DB().Where("notification_id = ?", n.ID.String()).Where("status = ?", models.NotificationStatusPending).Find(&pending).Error
				Expect(err).To(BeNil())

				return len(pending) == 1
			}, "15s", "1s", "should create a pending notification").Should(BeTrue())

			Eventually(func() int {
				_, err := notification.ProcessPendingNotifications(DefaultContext)
				Expect(err).To(BeNil())

				var sendHistory []models.NotificationSendHistory
				err = DefaultContext.DB().
					Where("notification_id = ?", n.ID.String()).
					Where("resource_id = ?", config.ID.String()).
					Where("source_event = ?", "config.warning").
					Where("status = ?", "skipped").
					Find(&sendHistory).Error
				Expect(err).To(BeNil())
				return len(sendHistory)
			}, "15s", "1s").Should(Equal(1))
		})
	})

	var _ = ginkgo.Describe("recent events", ginkgo.Ordered, func() {
		var n models.Notification
		var config models.ConfigItem
		var changes []models.ConfigChange

		ginkgo.BeforeAll(func() {
			n = models.Notification{
				ID:             uuid.New(),
				Name:           "recent-events",
				Events:         pq.StringArray([]string{"config.unhealthy"}),
				Source:         models.SourceCRD,
				Title:          "Dummy",
				Template:       "{{.recent_events}}",
				CustomServices: types.JSON(customReceiverJson),
			}

			err := DefaultContext.DB().Create(&n).Error
			Expect(err).To(BeNil())

			config = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("my-app"),
				ConfigClass: models.ConfigClassDeployment,
				Health:      lo.ToPtr(models.HealthHealthy),
				Config:      lo.ToPtr(`{"color": "red"}`),
				Type:        lo.ToPtr("Kubernetes::Deployment"),
			}

			err = DefaultContext.DB().Create(&config).Error
			Expect(err).To(BeNil())

			changes = []models.ConfigChange{
				{
					ConfigID:   config.ID.String(),
					ChangeType: "ScalingReplicaSet",
					Severity:   "info",
					Source:     "Kubernetes",
					Summary:    "Scaling replica set",
					Details:    types.JSON(`{"reason": "ScalingReplicaSet"}`),
					CreatedAt:  lo.ToPtr(time.Now().Add(-time.Minute * 3)),
					Count:      1,
				},
				{
					ConfigID:   config.ID.String(),
					ChangeType: "FailedCreate",
					Severity:   "high",
					Source:     "Kubernetes",
					Summary:    "Error creating: pods \"my-app-74d4c6dcf4-vb4jk\" is forbidden: exceeded quota: compute-resources",
					Details:    types.JSON(`{"reason": "FailedCreate"}`),
					CreatedAt:  lo.ToPtr(time.Now().Add(-time.Minute * 2)),
					Count:      1,
				},
				{
					ConfigID:   config.ID.String(),
					ChangeType: "Unhealthy",
					Severity:   "medium",
					Source:     "Kubernetes",
					Summary:    "Readiness probe failed: HTTP probe failed with statuscode: 500",
					Details:    types.JSON(`{"reason": "Unhealthy"}`),
					CreatedAt:  lo.ToPtr(time.Now().Add(-time.Minute)),
					Count:      1,
				},
				{
					ConfigID:   config.ID.String(),
					ChangeType: "ProgressDeadlineExceeded",
					Severity:   "high",
					Source:     "Kubernetes",
					Summary:    "Deployment my-app has not progressed in 10 minutes",
					Details:    types.JSON(`{"reason": "ProgressDeadlineExceeded"}`),
					CreatedAt:  lo.ToPtr(time.Now().Add(-time.Second * 30)),
					Count:      1,
				},
			}

			err = DefaultContext.DB().Create(&changes).Error
			Expect(err).To(BeNil())

			events.ConsumeAll(DefaultContext)
		})

		ginkgo.AfterAll(func() {
			err := DefaultContext.DB().Delete(&n).Error
			Expect(err).To(BeNil())

			err = DefaultContext.DB().Delete(&changes).Error
			Expect(err).To(BeNil())

			err = DefaultContext.DB().Delete(&config).Error
			Expect(err).To(BeNil())

			notification.PurgeCache(n.ID.String())
		})

		ginkgo.It("should create a new send history with recent events", func() {
			err := DefaultContext.DB().Model(&models.ConfigItem{}).Where("id = ?", config.ID).Update("health", models.HealthUnhealthy).Error
			Expect(err).To(BeNil())

			events.ConsumeAll(DefaultContext)

			Eventually(func() bool {
				var sendHistory []models.NotificationSendHistory
				err := DefaultContext.DB().
					Where("notification_id = ?", n.ID.String()).
					Where("resource_id = ?", config.ID.String()).
					Where("source_event = ?", "config.unhealthy").
					Find(&sendHistory).Error
				Expect(err).To(BeNil())

				return len(sendHistory) == 1 && lo.FromPtr(sendHistory[0].Body) == "[FailedCreate ProgressDeadlineExceeded Unhealthy]"
			}, "10s", "200ms").Should(BeTrue())
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
				templater := DefaultContext.NewStructTemplater(celEnv.AsMap(DefaultContext), "", notification.TemplateFuncs)
				err = templater.Walk(&msg)
				Expect(err).To(BeNil())

				var slackBlock map[string]any
				err = json.Unmarshal([]byte(msg.Message), &slackBlock)
				Expect(err).To(BeNil())
			})
		}
	})

	var _ = ginkgo.Describe("group notifications", func() {
		var n models.Notification
		var config1, config2, config3, config4 models.ConfigItem
		ginkgo.BeforeAll(func() {
			n = models.Notification{
				ID:             uuid.New(),
				Name:           "group-by-test",
				Events:         pq.StringArray([]string{"config.unhealthy"}),
				Source:         models.SourceCRD,
				Title:          "Dummy",
				Template:       "Failed: $(.config.id)/$(.config.name)",
				CustomServices: types.JSON(customReceiverJson),
				WaitFor:        lo.ToPtr(time.Second * 15),
				GroupBy:        pq.StringArray{"description", "type"},
			}

			err := DefaultContext.DB().Create(&n).Error
			Expect(err).To(BeNil())

			config1 = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("config1"),
				ConfigClass: "HelmRelease",
				Health:      lo.ToPtr(models.HealthHealthy),
				Config:      lo.ToPtr(`{"color": "red"}`),
				Type:        lo.ToPtr("Kubernetes::HelmRelease"),
			}
			config2 = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("config2"),
				ConfigClass: "HelmRelease",
				Health:      lo.ToPtr(models.HealthHealthy),
				Config:      lo.ToPtr(`{"color": "red"}`),
				Type:        lo.ToPtr("Kubernetes::HelmRelease"),
			}
			config3 = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("config3"),
				ConfigClass: "HelmRelease",
				Health:      lo.ToPtr(models.HealthHealthy),
				Config:      lo.ToPtr(`{"color": "red"}`),
				Type:        lo.ToPtr("Kubernetes::HelmRelease"),
			}
			config4 = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("config4"),
				ConfigClass: "HelmRelease",
				Health:      lo.ToPtr(models.HealthHealthy),
				Config:      lo.ToPtr(`{"color": "red"}`),
				Type:        lo.ToPtr("Kubernetes::HelmRelease"),
			}
			err = DefaultContext.DB().Create(&config1).Error
			Expect(err).To(BeNil())
			err = DefaultContext.DB().Create(&config2).Error
			Expect(err).To(BeNil())
			err = DefaultContext.DB().Create(&config3).Error
			Expect(err).To(BeNil())
			err = DefaultContext.DB().Create(&config4).Error
			Expect(err).To(BeNil())

			events.ConsumeAll(DefaultContext)
		})

		ginkgo.AfterAll(func() {
			err := DefaultContext.DB().Delete(&n).Error
			Expect(err).To(BeNil())

			err = DefaultContext.DB().Delete(&config1).Error
			Expect(err).To(BeNil())
			err = DefaultContext.DB().Delete(&config2).Error
			Expect(err).To(BeNil())
			err = DefaultContext.DB().Delete(&config3).Error
			Expect(err).To(BeNil())
			err = DefaultContext.DB().Delete(&config4).Error
			Expect(err).To(BeNil())

			notification.PurgeCache(n.ID.String())
		})

		ginkgo.It("should group config resources in a notification", func() {
			for _, c := range []models.ConfigItem{config1, config2, config3, config4} {
				err := DefaultContext.DB().Model(&models.ConfigItem{}).
					Where("id = ?", c.ID).
					UpdateColumns(map[string]any{
						"health":      models.HealthUnhealthy,
						"description": fmt.Sprintf("%s is failing due to bad manifest", *c.Name),
					}).Error
				Expect(err).To(BeNil())
			}
			events.ConsumeAll(DefaultContext)

			time.Sleep(3 * time.Second)

			// Mark config3 as healthy to ensure healthy configs are skipped
			err := DefaultContext.DB().Model(&models.ConfigItem{}).
				Where("id = ?", config3.ID).
				UpdateColumns(map[string]any{"health": models.HealthHealthy, "description": "healthy"}).Error
			Expect(err).To(BeNil())

			events.ConsumeAll(DefaultContext)

			time.Sleep(12 * time.Second)

			_, err = notification.ProcessPendingNotifications(DefaultContext)
			Expect(err).To(BeNil())
			Eventually(func() bool {
				var histories []models.NotificationSendHistory
				err = DefaultContext.DB().Where("notification_id = ?", n.ID.String()).
					Where("status NOT IN ?", []any{models.NotificationStatusSent, models.NotificationStatusSkipped}).
					Find(&histories).Error
				Expect(err).To(BeNil())
				return len(histories) == 0
			}, "5s", "1s").Should(BeTrue())

			Eventually(func() int {
				return len(webhookPostdata)
			}, "10s", "200ms").Should(BeNumerically(">=", 1))

			Expect(webhookPostdata).To(Not(BeNil()))

			msg := webhookPostdata["message"]
			msgBlocks := strings.Split(msg, "Resources grouped with notification:\n")
			Expect(len(msgBlocks)).To(Equal(2))

			groupedResources := strings.Split(msgBlocks[1], "\n")
			// 2 other configs since 1 config is part of the original message
			Expect(len(groupedResources)).To(Equal(2))

			// All config names should be present
			Expect(msg).To(ContainSubstring(*config1.Name))
			Expect(msg).To(ContainSubstring(*config2.Name))
			Expect(msg).To(ContainSubstring(*config4.Name))
		})
	})

	var _ = ginkgo.Describe("Dedup skipped notifications", func() {
		var sendHistory models.NotificationSendHistory
		var n models.Notification
		var config models.ConfigItem

		ginkgo.BeforeAll(func() {
			n = models.Notification{
				ID:             uuid.New(),
				Name:           "group-by-test",
				Events:         pq.StringArray([]string{"config.unhealthy"}),
				Source:         models.SourceCRD,
				Title:          "Dummy",
				Template:       "Failed: $(.config.id)/$(.config.name)",
				CustomServices: types.JSON(customReceiverJson),
				WaitFor:        lo.ToPtr(time.Second * 15),
			}

			err := DefaultContext.DB().Create(&n).Error
			Expect(err).To(BeNil())

			config = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("config1"),
				ConfigClass: "HelmRelease",
				Health:      lo.ToPtr(models.HealthHealthy),
				Config:      lo.ToPtr(`{"color": "red"}`),
				Type:        lo.ToPtr("Kubernetes::HelmRelease"),
			}

			err = DefaultContext.DB().Create(&config).Error
			Expect(err).To(BeNil())

			// Assume there's a notification that's currently evaluating a waitfor
			sendHistory = models.NotificationSendHistory{
				NotificationID: n.ID,
				ResourceID:     config.ID,
				SourceEvent:    "config.unhealthy",
				Status:         models.NotificationStatusEvaluatingWaitFor,
				CreatedAt:      time.Now().Add(-time.Second * 10),
			}

			err = DefaultContext.DB().Create(&sendHistory).Error
			Expect(err).To(BeNil())
		})

		ginkgo.It("should skip the notification if the resource is unhealthy", func() {
			err := db.SkipNotificationSendHistory(DefaultContext, sendHistory.ID)
			Expect(err).To(BeNil())

			var allSendHistories []models.NotificationSendHistory
			err = DefaultContext.DB().Where("notification_id = ?", n.ID.String()).Find(&allSendHistories).Error
			Expect(err).To(BeNil())
			Expect(len(allSendHistories)).To(Equal(1))
			Expect(allSendHistories[0].Status).To(Equal(models.NotificationStatusSkipped))
		})

		ginkgo.It("try to skip another send history", func() {
			// Assume there's a notification that's currently evaluating a waitfor
			anotherSendHistory := models.NotificationSendHistory{
				NotificationID: n.ID,
				ResourceID:     config.ID,
				SourceEvent:    "config.unhealthy",
				Status:         models.NotificationStatusEvaluatingWaitFor,
				CreatedAt:      time.Now().Add(-time.Second * 10),
			}

			err := DefaultContext.DB().Create(&anotherSendHistory).Error
			Expect(err).To(BeNil())

			err = db.SkipNotificationSendHistory(DefaultContext, anotherSendHistory.ID)
			Expect(err).To(BeNil())

			var allSendHistories []models.NotificationSendHistory
			err = DefaultContext.DB().Where("notification_id = ?", n.ID.String()).Find(&allSendHistories).Error
			Expect(err).To(BeNil())
			Expect(len(allSendHistories)).To(Equal(1))
			Expect(allSendHistories[0].Status).To(Equal(models.NotificationStatusSkipped))
			Expect(allSendHistories[0].Count).To(Equal(2))
		})
	})
})
