package notification

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	dbModels "github.com/flanksource/incident-commander/db/models"
	"github.com/flanksource/incident-commander/events"
	"github.com/google/uuid"
	"github.com/lib/pq"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
			Components: []api.ComponentSelector{{Name: "logistics"}},
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
		logger.Infof("before consume")
		events.ConsumeAll(DefaultContext)
		logger.Infof("after consume")
		Eventually(func() int {
			logger.Infof("checking")
			return len(webhookPostdata)
		}, "10s", "200ms").Should(BeNumerically(">=", 1))

		Expect(webhookPostdata).To(Not(BeNil()))
		Expect(webhookPostdata["message"]).To(Equal(fmt.Sprintf("Severity: %s", incident.Severity)))
		Expect(webhookPostdata["title"]).To(Equal(fmt.Sprintf("New incident: %s", incident.Title)))
	})
})
