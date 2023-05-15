package main

import (
	"encoding/json"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/jobs"
	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type dummyConfig struct {
	Name      string `json:"name"`
	Threshold int    `json:"threshold"`
}

func (t dummyConfig) String() *string {
	b, _ := json.Marshal(t)
	response := string(b)
	return &response
}

var _ = ginkgo.Describe("Config Done Test", ginkgo.Ordered, func() {
	var (
		john       *models.Person
		incident   *models.Incident
		component  *models.Component
		configItem *models.ConfigItem
		hypothesis *models.Hypothesis
		evidence   *models.Evidence
		config     = dummyConfig{
			Name:      "my dummy config",
			Threshold: 50,
		}
	)

	ginkgo.It("should create a person", func() {
		john = &models.Person{
			ID:   uuid.New(),
			Name: "John Doe",
		}
		tx := db.Gorm.Create(john)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create a new component", func() {
		component = &models.Component{
			ID:         uuid.New(),
			Name:       "logistics",
			Type:       "Entity",
			ExternalId: "dummy/logistics",
		}
		tx := db.Gorm.Create(component)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create a config", func() {
		configItem = &models.ConfigItem{
			ID:          uuid.New(),
			ConfigClass: "MyConfigClass",
			Config:      config.String(),
		}
		tx := db.Gorm.Create(configItem)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create an incident", func() {
		incident = &models.Incident{
			ID:          uuid.New(),
			Title:       "Constantly hitting threshold",
			CreatedBy:   john.ID,
			Type:        models.IncidentTypeAvailability,
			Status:      models.IncidentStatusOpen,
			Severity:    "Blocker",
			CommanderID: &john.ID,
		}
		tx := db.Gorm.Create(incident)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create a new hypothesis", func() {
		hypothesis = &models.Hypothesis{
			ID:         uuid.New(),
			IncidentID: incident.ID,
			Title:      "Threshold could safely be increased to 80",
			CreatedBy:  john.ID,
			Type:       "solution",
			Status:     "possible",
		}
		tx := db.Gorm.Create(hypothesis)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create a new evidence from the config", func() {
		evidence = &models.Evidence{
			ID:               uuid.New(),
			HypothesisID:     hypothesis.ID,
			ComponentID:      &component.ID,
			CreatedBy:        john.ID,
			Description:      "Logisctics DB attached component",
			Type:             "component",
			Script:           "config.threshold >= 80",
			ConfigID:         &configItem.ID,
			DefinitionOfDone: true,
		}
		tx := db.Gorm.Create(evidence)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("modify the config but doesn't satisfy the done definition", func() {
		config.Threshold = 75
		configItem.Config = config.String()
		tx := db.Gorm.Save(configItem)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should NOT mark the evidence as done", func() {
		jobs.EvaluateEvidenceScripts()

		var fetchedIncident models.Incident
		err := db.Gorm.Find(&fetchedIncident).Where("id = ?", evidence.ID).Error
		Expect(err).To(BeNil())

		Expect(fetchedIncident.Status).To(Equal(models.IncidentStatusOpen))
	})

	ginkgo.It("modify the config", func() {
		config.Threshold = 85
		configItem.Config = config.String()
		tx := db.Gorm.Save(configItem)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should mark the evidence as done", func() {
		jobs.EvaluateEvidenceScripts()

		var fetchedIncident models.Incident
		err := db.Gorm.Find(&fetchedIncident).Where("id = ?", evidence.ID).Error
		Expect(err).To(BeNil())

		Expect(fetchedIncident.Status).To(Equal(models.IncidentStatusResolved))
	})
})
