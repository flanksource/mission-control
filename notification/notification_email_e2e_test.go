package notification_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/lib/pq"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	dbModels "github.com/flanksource/incident-commander/db/models"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/notification"
)

func lastSMTPMessage() string {
	messages := getSMTPMessages()
	if len(messages) == 0 {
		return ""
	}
	return string(messages[len(messages)-1].Data)
}

var _ = ginkgo.Describe("Notification email end-to-end", ginkgo.Ordered, func() {
	ginkgo.BeforeEach(func() {
		clearSMTPMessages()
	})

	ginkgo.Describe("config unhealthy default templates", ginkgo.Ordered, func() {
		var (
			n      models.Notification
			config models.ConfigItem
		)

		ginkgo.BeforeAll(func() {
			receivers := []api.NotificationConfig{
				{
					URL: fmt.Sprintf("%s?ToAddresses=%s", api.SystemSMTP, url.QueryEscape("config-unhealthy@flanksource.com")),
				},
			}
			receiverJSON, err := json.Marshal(receivers)
			Expect(err).To(BeNil())

			n = models.Notification{
				ID:             uuid.New(),
				Name:           "config-unhealthy-email-default",
				Events:         pq.StringArray{api.EventConfigUnhealthy},
				Source:         models.SourceCRD,
				CustomServices: types.JSON(receiverJSON),
			}
			Expect(DefaultContext.DB().Create(&n).Error).To(BeNil())

			config = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("api-server"),
				ConfigClass: models.ConfigClassDeployment,
				Health:      lo.ToPtr(models.HealthHealthy),
				Config:      lo.ToPtr(`{"color": "red"}`),
				Type:        lo.ToPtr("Kubernetes::Deployment"),
			}
			Expect(DefaultContext.DB().Create(&config).Error).To(BeNil())
		})

		ginkgo.AfterAll(func() {
			Expect(DefaultContext.DB().Delete(&n).Error).To(BeNil())
			Expect(DefaultContext.DB().Delete(&config).Error).To(BeNil())
			notification.PurgeCache(n.ID.String())
		})

		ginkgo.It("sends default template email", func() {
			event := models.Event{
				Name: api.EventConfigUnhealthy,
				Properties: types.JSONStringMap{
					"id":          config.ID.String(),
					"description": "Readiness probe failed",
					"status":      "CrashLoopBackOff",
				},
			}
			Expect(DefaultContext.DB().Create(&event).Error).To(BeNil())

			events.ConsumeAll(DefaultContext)

			Eventually(func() int {
				return len(getSMTPMessages())
			}, "10s", "200ms").Should(Equal(1))

			msg := lastSMTPMessage()
			Expect(msg).To(ContainSubstring("Kubernetes::Deployment api-server is unhealthy"))
			Expect(msg).To(ContainSubstring("Readiness probe failed"))
		})
	})

	ginkgo.Describe("check passed team email", ginkgo.Ordered, func() {
		var (
			n           models.Notification
			team        dbModels.Team
			creator     models.Person
			agent       models.Agent
			canary      models.Canary
			check       models.Check
			checkRun    models.CheckStatus
			lastRuntime string
		)

		ginkgo.BeforeAll(func() {
			creator = models.Person{
				ID:    uuid.New(),
				Name:  "Email Team Owner",
				Email: "team-owner@flanksource.com",
			}
			Expect(DefaultContext.DB().Create(&creator).Error).To(BeNil())

			agent = models.Agent{
				ID:   uuid.New(),
				Name: "email-agent",
			}
			Expect(DefaultContext.DB().Create(&agent).Error).To(BeNil())

			canary = models.Canary{
				ID:        uuid.New(),
				Name:      "email-canary",
				Namespace: "default",
				AgentID:   agent.ID,
				Spec:      types.JSON(`{}`),
			}
			Expect(DefaultContext.DB().Create(&canary).Error).To(BeNil())

			check = models.Check{
				ID:        uuid.New(),
				Name:      "HTTP 200",
				Namespace: "default",
				Type:      "http",
				CanaryID:  canary.ID,
				AgentID:   agent.ID,
				Spec:      types.JSON(`{}`),
			}
			Expect(DefaultContext.DB().Create(&check).Error).To(BeNil())

			teamSpec := api.TeamSpec{
				Notifications: []api.NotificationConfig{
					{
						Name: "email",
						URL:  fmt.Sprintf("%s?ToAddresses=%s", api.SystemSMTP, url.QueryEscape("team@flanksource.com")),
					},
				},
			}
			specRaw, err := collections.StructToJSON(teamSpec)
			Expect(err).To(BeNil())

			var spec types.JSONMap
			Expect(json.Unmarshal([]byte(specRaw), &spec)).To(Succeed())

			team = dbModels.Team{
				ID:        uuid.New(),
				Name:      "email-team",
				CreatedBy: creator.ID,
				Spec:      spec,
			}
			Expect(DefaultContext.DB().Create(&team).Error).To(BeNil())

			n = models.Notification{
				ID:       uuid.New(),
				Name:     "check-passed-team-email",
				Events:   pq.StringArray{api.EventCheckPassed},
				Source:   models.SourceCRD,
				Title:    "Check OK: {{.check.name}}",
				Template: "Status: {{.check_status.message}}",
				TeamID:   &team.ID,
			}
			Expect(DefaultContext.DB().Create(&n).Error).To(BeNil())
		})

		ginkgo.AfterAll(func() {
			Expect(DefaultContext.DB().Where("check_id = ?", check.ID).Delete(&models.CheckStatus{}).Error).To(BeNil())
			Expect(DefaultContext.DB().Delete(&n).Error).To(BeNil())
			Expect(DefaultContext.DB().Delete(&team).Error).To(BeNil())
			Expect(DefaultContext.DB().Delete(&check).Error).To(BeNil())
			Expect(DefaultContext.DB().Delete(&canary).Error).To(BeNil())
			Expect(DefaultContext.DB().Delete(&agent).Error).To(BeNil())
			Expect(DefaultContext.DB().Delete(&creator).Error).To(BeNil())
			notification.PurgeCache(n.ID.String())
		})

		ginkgo.It("sends custom team email", func() {
			lastRuntime = time.Now().UTC().Format(time.DateTime)
			checkRun = models.CheckStatus{
				CheckID: check.ID,
				Status:  true,
				Time:    lastRuntime,
				Message: "All good",
			}
			Expect(DefaultContext.DB().Create(&checkRun).Error).To(BeNil())

			event := models.Event{
				Name: api.EventCheckPassed,
				Properties: types.JSONStringMap{
					"id":           check.ID.String(),
					"last_runtime": lastRuntime,
				},
			}
			Expect(DefaultContext.DB().Create(&event).Error).To(BeNil())

			events.ConsumeAll(DefaultContext)

			Eventually(func() int {
				return len(getSMTPMessages())
			}, "10s", "200ms").Should(Equal(1))

			msg := lastSMTPMessage()
			Expect(msg).To(ContainSubstring("Check OK: HTTP 200"))
			Expect(msg).To(ContainSubstring("All good"))
		})
	})

	ginkgo.Describe("check failed person email", ginkgo.Ordered, func() {
		var (
			n           models.Notification
			person      models.Person
			agent       models.Agent
			canary      models.Canary
			check       models.Check
			checkRun    models.CheckStatus
			lastRuntime string
		)

		ginkgo.BeforeAll(func() {
			person = models.Person{
				ID:    uuid.New(),
				Name:  "Alert Receiver",
				Email: "alerts@flanksource.com",
			}
			Expect(DefaultContext.DB().Create(&person).Error).To(BeNil())

			agent = models.Agent{
				ID:   uuid.New(),
				Name: "failure-agent",
			}
			Expect(DefaultContext.DB().Create(&agent).Error).To(BeNil())

			canary = models.Canary{
				ID:        uuid.New(),
				Name:      "failure-canary",
				Namespace: "default",
				AgentID:   agent.ID,
				Spec:      types.JSON(`{}`),
			}
			Expect(DefaultContext.DB().Create(&canary).Error).To(BeNil())

			check = models.Check{
				ID:        uuid.New(),
				Name:      "HTTP 500",
				Namespace: "default",
				Type:      "http",
				CanaryID:  canary.ID,
				AgentID:   agent.ID,
				Spec:      types.JSON(`{}`),
			}
			Expect(DefaultContext.DB().Create(&check).Error).To(BeNil())

			n = models.Notification{
				ID:       uuid.New(),
				Name:     "check-failed-person-email",
				Events:   pq.StringArray{api.EventCheckFailed},
				Source:   models.SourceCRD,
				PersonID: &person.ID,
			}
			Expect(DefaultContext.DB().Create(&n).Error).To(BeNil())
		})

		ginkgo.AfterAll(func() {
			Expect(DefaultContext.DB().Where("check_id = ?", check.ID).Delete(&models.CheckStatus{}).Error).To(BeNil())
			Expect(DefaultContext.DB().Delete(&n).Error).To(BeNil())
			Expect(DefaultContext.DB().Delete(&check).Error).To(BeNil())
			Expect(DefaultContext.DB().Delete(&canary).Error).To(BeNil())
			Expect(DefaultContext.DB().Delete(&agent).Error).To(BeNil())
			Expect(DefaultContext.DB().Delete(&person).Error).To(BeNil())
			notification.PurgeCache(n.ID.String())
		})

		ginkgo.It("sends default person email", func() {
			lastRuntime = time.Now().UTC().Format(time.DateTime)
			checkRun = models.CheckStatus{
				CheckID: check.ID,
				Status:  false,
				Time:    lastRuntime,
				Error:   "Timeout",
			}
			Expect(DefaultContext.DB().Create(&checkRun).Error).To(BeNil())

			event := models.Event{
				Name: api.EventCheckFailed,
				Properties: types.JSONStringMap{
					"id":           check.ID.String(),
					"last_runtime": lastRuntime,
				},
			}
			Expect(DefaultContext.DB().Create(&event).Error).To(BeNil())

			events.ConsumeAll(DefaultContext)

			Eventually(func() int {
				return len(getSMTPMessages())
			}, "10s", "200ms").Should(Equal(1))

			msg := lastSMTPMessage()
			Expect(msg).To(ContainSubstring("Check HTTP 500 has failed"))
			Expect(msg).To(ContainSubstring("Timeout"))
		})
	})
})
