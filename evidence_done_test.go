package main

import (
	"encoding/json"
	"time"

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

var _ = ginkgo.Describe("Test Incident Done Definition With Config Item", ginkgo.Ordered, func() {
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

	ginkgo.It("modify the config but do not satisfy the done definition", func() {
		config.Threshold = 75
		configItem.Config = config.String()
		tx := db.Gorm.Save(configItem)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should NOT mark the incident as resolved", func() {
		jobs.EvaluateEvidenceScripts()

		var fetchedIncident models.Incident
		err := db.Gorm.Where("id = ?", incident.ID).First(&fetchedIncident).Error
		Expect(err).To(BeNil())

		Expect(fetchedIncident.Status).To(Equal(models.IncidentStatusOpen))
	})

	ginkgo.It("modify the config", func() {
		config.Threshold = 85
		configItem.Config = config.String()
		tx := db.Gorm.Save(configItem)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should mark the incident as resolved", func() {
		jobs.EvaluateEvidenceScripts()

		var fetchedIncident models.Incident
		err := db.Gorm.Where("id = ?", incident.ID).First(&fetchedIncident).Error
		Expect(err).To(BeNil())

		Expect(fetchedIncident.Status).To(Equal(models.IncidentStatusResolved))
	})
})

var _ = ginkgo.Describe("Test Incident Done Definition With Config Item", ginkgo.Ordered, func() {
	var (
		john           *models.Person
		incident       *models.Incident
		configItem     *models.ConfigItem
		configAnalysis *models.ConfigAnalysis
		component      *models.Component
		hypothesis     *models.Hypothesis
		evidence       *models.Evidence
		config         = dummyConfig{
			Name:      "my dummy config",
			Threshold: 50,
		}
	)

	ginkgo.It("should create a person", func() {
		john = &models.Person{
			ID:   uuid.New(),
			Name: "James Bond",
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

	ginkgo.It("should create a config config analysis", func() {
		configAnalysis = &models.ConfigAnalysis{
			ID:           uuid.New(),
			ConfigID:     configItem.ID,
			Summary:      "Right-size or shutdown underutilized virtual machines",
			AnalysisType: "cost",
			Severity:     "high",
		}
		tx := db.Gorm.Create(configAnalysis)
		Expect(tx.Error).To(BeNil())
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
		tx := db.Gorm.Create(incident)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create a new hypothesis", func() {
		hypothesis = &models.Hypothesis{
			ID:         uuid.New(),
			IncidentID: incident.ID,
			Title:      "can scale down to 3 instances",
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
			CreatedBy:        john.ID,
			Description:      "Azure Advisor recommends shutting down underutilized virtual machines",
			Script:           "analysis.status == 'resolved'",
			ConfigAnalysisID: &configAnalysis.ID,
			DefinitionOfDone: true,
		}
		tx := db.Gorm.Create(evidence)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("modify the config analysis but do not satisfy the done definition", func() {
		configAnalysis.Status = "open"
		tx := db.Gorm.Save(configAnalysis)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should NOT mark the incident as resolved", func() {
		jobs.EvaluateEvidenceScripts()

		var fetchedIncident models.Incident
		err := db.Gorm.Where("id = ?", incident.ID).First(&fetchedIncident).Error
		Expect(err).To(BeNil())

		Expect(fetchedIncident.Status).To(Equal(models.IncidentStatusOpen))
	})

	ginkgo.It("modify the config analysis", func() {
		configAnalysis.Status = "resolved"
		tx := db.Gorm.Save(configAnalysis)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should mark the incident as resolved", func() {
		jobs.EvaluateEvidenceScripts()

		var fetchedIncident models.Incident
		err := db.Gorm.Where("id = ?", incident.ID).First(&fetchedIncident).Error
		Expect(err).To(BeNil())

		Expect(fetchedIncident.Status).To(Equal(models.IncidentStatusResolved))
	})
})

var _ = ginkgo.Describe("Test Incident Done Definition With Health Check", ginkgo.Ordered, func() {
	var (
		john       *models.Person
		component  *models.Component
		canary     *models.Canary
		check      *models.Check
		incident   *models.Incident
		hypothesis *models.Hypothesis
		evidence   *models.Evidence
	)

	ginkgo.It("should create a person", func() {
		john = &models.Person{
			ID:   uuid.New(),
			Name: "John Wick",
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

	ginkgo.It("should create a canary", func() {
		canary = &models.Canary{
			ID:        uuid.New(),
			Name:      "flanksource homepage check",
			Namespace: "default",
			Spec:      []byte("{}"),
			CreatedAt: time.Now(),
		}
		tx := db.Gorm.Create(canary)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create a check", func() {
		check = &models.Check{
			ID:       uuid.New(),
			CanaryID: canary.ID,
			Name:     "flanksource-homepage-check",
			Type:     "http",
			Status:   "unhealthy",
		}
		tx := db.Gorm.Create(check)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create an incident", func() {
		incident = &models.Incident{
			ID:          uuid.New(),
			Title:       "Site is down",
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
			Title:      "Have you tried turning it off and on again?",
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
			Script:           `check.status == "healthy" && check.age > duration("30s")`,
			CheckID:          &check.ID,
			DefinitionOfDone: true,
		}
		tx := db.Gorm.Create(evidence)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("pretend the site has been up for 15 seconds", func() {
		check.Status = "healthy"
		past := time.Now().Add(-time.Second * 15)
		check.LastTransitionTime = &past
		tx := db.Gorm.Save(check)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should NOT mark the incident as resolved", func() {
		jobs.EvaluateEvidenceScripts()

		var fetchedIncident models.Incident
		err := db.Gorm.Where("id = ?", incident.ID).First(&fetchedIncident).Error
		Expect(err).To(BeNil())

		Expect(fetchedIncident.Status).To(Equal(models.IncidentStatusOpen))
	})

	ginkgo.It("pretend the site has been up for 31 seconds", func() {
		past := time.Now().Add(-time.Second * 31)
		check.LastTransitionTime = &past
		tx := db.Gorm.Save(check)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should mark the incident as resolved", func() {
		jobs.EvaluateEvidenceScripts()

		var fetchedIncident models.Incident
		err := db.Gorm.Where("id = ?", incident.ID).First(&fetchedIncident).Error
		Expect(err).To(BeNil())

		Expect(fetchedIncident.Status).To(Equal(models.IncidentStatusResolved))
	})
})
