package incidents

import (
	"encoding/json"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/setup"
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

var _ = ginkgo.Describe("Incident Definition of Done", ginkgo.Ordered, func() {
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

	ginkgo.It("should setup", func() {
		john = &models.Person{
			ID:   uuid.New(),
			Name: "John Doe",
		}
		Expect(DefaultContext.DB().Create(john).Error).To(BeNil())

		component = &models.Component{
			ID:         uuid.New(),
			Name:       "logistics",
			Type:       "Entity",
			ExternalId: "dummy/logistics",
		}
		Expect(DefaultContext.DB().Create(component).Error).To(BeNil())

		configItem = &models.ConfigItem{
			ID:          uuid.New(),
			ConfigClass: "MyConfigClass",
			Config:      config.String(),
		}
		Expect(DefaultContext.DB().Create(configItem).Error).To(BeNil())
		EvaluateEvidence.Context = DefaultContext

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
		Expect(DefaultContext.DB().Create(incident).Error).To(BeNil())
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
		tx := DefaultContext.DB().Create(hypothesis)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create a new evidence from the config", func() {
		evidence = &models.Evidence{
			ID:               uuid.New(),
			HypothesisID:     hypothesis.ID,
			ComponentID:      &component.ID,
			CreatedBy:        john.ID,
			Description:      "Logistics DB attached component",
			Type:             "component",
			Script:           "config.threshold >= 80",
			ConfigID:         &configItem.ID,
			DefinitionOfDone: true,
		}
		tx := DefaultContext.DB().Create(evidence)
		Expect(tx.Error).To(BeNil())

		// Another evidence done definition but empty script. This should be ignored.
		evidenceWithEmptyScript := &models.Evidence{
			ID:               uuid.New(),
			HypothesisID:     hypothesis.ID,
			ComponentID:      &component.ID,
			CreatedBy:        john.ID,
			Description:      "Something went wrong",
			Type:             "component",
			Script:           "",
			ConfigID:         &configItem.ID,
			DefinitionOfDone: true,
		}
		tx = DefaultContext.DB().Create(evidenceWithEmptyScript)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("modify the config but do not satisfy the done definition", func() {
		config.Threshold = 75
		configItem.Config = config.String()
		tx := DefaultContext.DB().Save(configItem)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should NOT mark the incident as resolved", func() {
		EvaluateEvidence.Run()
		setup.ExpectJobToPass(EvaluateEvidence)

		var fetchedIncident models.Incident
		err := DefaultContext.DB().Where("id = ?", incident.ID).First(&fetchedIncident).Error
		Expect(err).To(BeNil())

		Expect(fetchedIncident.Status).To(Equal(models.IncidentStatusOpen))
	})

	ginkgo.It("modify the config", func() {
		config.Threshold = 85
		configItem.Config = config.String()
		tx := DefaultContext.DB().Save(configItem)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should mark the incident as resolved", func() {
		EvaluateEvidence.Run()
		setup.ExpectJobToPass(EvaluateEvidence)

		var fetchedIncident models.Incident
		err := DefaultContext.DB().Where("id = ?", incident.ID).First(&fetchedIncident).Error
		Expect(err).To(BeNil())

		Expect(fetchedIncident.Status).To(Equal(models.IncidentStatusResolved))
	})
})

var _ = ginkgo.Describe("Incident Definition of Done Config Item", ginkgo.Ordered, func() {
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
		EvaluateEvidence.Context = DefaultContext
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

	ginkgo.It("should create a config", func() {
		configItem = &models.ConfigItem{
			ID:          uuid.New(),
			ConfigClass: "MyConfigClass",
			Config:      config.String(),
		}
		tx := DefaultContext.DB().Create(configItem)
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
		tx := DefaultContext.DB().Create(configAnalysis)
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
		tx := DefaultContext.DB().Create(incident)
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
		tx := DefaultContext.DB().Create(hypothesis)
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
		tx := DefaultContext.DB().Create(evidence)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("modify the config analysis but do not satisfy the done definition", func() {
		configAnalysis.Status = "open"
		tx := DefaultContext.DB().Save(configAnalysis)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should NOT mark the incident as resolved", func() {
		EvaluateEvidence.Run()
		setup.ExpectJobToPass(EvaluateEvidence)

		var fetchedIncident models.Incident
		err := DefaultContext.DB().Where("id = ?", incident.ID).First(&fetchedIncident).Error
		Expect(err).To(BeNil())

		Expect(fetchedIncident.Status).To(Equal(models.IncidentStatusOpen))
	})

	ginkgo.It("modify the config analysis", func() {
		configAnalysis.Status = "resolved"
		tx := DefaultContext.DB().Save(configAnalysis)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should mark the incident as resolved", func() {
		EvaluateEvidence.Run()
		setup.ExpectJobToPass(EvaluateEvidence)

		var fetchedIncident models.Incident
		err := DefaultContext.DB().Where("id = ?", incident.ID).First(&fetchedIncident).Error
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

	ginkgo.It("should create a canary", func() {
		canary = &models.Canary{
			ID:        uuid.New(),
			Name:      "flanksource homepage check",
			Namespace: "default",
			Spec:      []byte("{}"),
			CreatedAt: time.Now(),
		}
		tx := DefaultContext.DB().Create(canary)
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
		tx := DefaultContext.DB().Create(check)
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
		tx := DefaultContext.DB().Create(incident)
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
		tx := DefaultContext.DB().Create(hypothesis)
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
		tx := DefaultContext.DB().Create(evidence)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("pretend the site has been up for 15 seconds", func() {
		check.Status = "healthy"
		past := time.Now().Add(-time.Second * 15)
		check.LastTransitionTime = &past
		tx := DefaultContext.DB().Save(check)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should NOT mark the incident as resolved", func() {
		EvaluateEvidence.Context = DefaultContext
		EvaluateEvidence.Run()
		setup.ExpectJobToPass(EvaluateEvidence)

		var fetchedIncident models.Incident
		err := DefaultContext.DB().Where("id = ?", incident.ID).First(&fetchedIncident).Error
		Expect(err).To(BeNil())

		Expect(fetchedIncident.Status).To(Equal(models.IncidentStatusOpen))
	})

	ginkgo.It("pretend the site has been up for 31 seconds", func() {
		past := time.Now().Add(-time.Second * 31)
		check.LastTransitionTime = &past
		tx := DefaultContext.DB().Save(check)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should mark the incident as resolved", func() {
		EvaluateEvidence.Context = DefaultContext
		EvaluateEvidence.Run()
		setup.ExpectJobToPass(EvaluateEvidence)

		var fetchedIncident models.Incident
		err := DefaultContext.DB().Where("id = ?", incident.ID).First(&fetchedIncident).Error
		Expect(err).To(BeNil())

		Expect(fetchedIncident.Status).To(Equal(models.IncidentStatusResolved))
	})
})
