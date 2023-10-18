package main

import (
	gocontext "context"
	"fmt"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rules"
	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Test incident creation via incidence rule", ginkgo.Ordered, func() {
	var (
		incidentRule     *api.IncidentRule
		component        *models.Component
		anotherComponent *models.Component
	)

	const (
		namespace           = "incidentTests"
		incidentDescription = "This is an auto-generated incident"
	)

	ginkgo.It("should create a system user", func() {
		systemUser := models.Person{
			Name: "System",
		}
		tx := db.Gorm.Find(&systemUser)
		Expect(tx.Error).To(BeNil())

		api.SystemUserID = &systemUser.ID
	})

	ginkgo.It("should create components", func() {
		component = &models.Component{
			ID:         uuid.New(),
			Name:       "Component For Rule",
			Type:       "Entity",
			ExternalId: "dummy/component_that_will_fail",
			Namespace:  namespace,
		}
		tx := db.Gorm.Create(component)
		Expect(tx.Error).To(BeNil())

		anotherComponent = &models.Component{
			ID:         uuid.New(),
			Name:       "Another Component For Rule",
			Type:       "Entity",
			ExternalId: "dummy/another_component_that_will_fail",
			Namespace:  namespace,
		}
		tx = db.Gorm.Create(anotherComponent)
		Expect(tx.Error).To(BeNil())

		componentHealthy := &models.Component{
			ID:         uuid.New(),
			Name:       "Healthy component",
			Type:       "Entity",
			ExternalId: "dummy/healthy_component",
			Status:     types.ComponentStatusHealthy,
			Namespace:  namespace,
		}
		tx = db.Gorm.Create(componentHealthy)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create an incident rule", func() {
		incidentRule = &api.IncidentRule{
			ID:   uuid.New(),
			Name: "My incident rule",
			Spec: &api.IncidentRuleSpec{
				Name: "First spec",
				Filter: api.Filter{
					Status: []string{string(types.ComponentStatusUnhealthy), string(types.ComponentStatusError)},
				},
				Components: []api.ComponentSelector{{Namespace: namespace}},
				Template: api.IncidentTemplate{
					Description: incidentDescription,
				},
				IncidentResponders: api.IncidentResponders{
					Email: []api.Email{
						{
							To:      "contact@flanksource.com",
							Subject: "New incident",
							Body:    "please check",
						},
					},
				},
			},
			CreatedAt: time.Now(),
		}
		tx := db.Gorm.Create(incidentRule)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should mark the components as bad", func() {
		component.Status = types.ComponentStatusUnhealthy
		tx := db.Gorm.Save(component)
		Expect(tx.Error).To(BeNil())

		anotherComponent.Status = types.ComponentStatusError
		tx = db.Gorm.Save(anotherComponent)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create incidents", func() {
		ctx := context.NewContext(gocontext.Background()).WithDB(db.Gorm, db.Pool)
		jobCtx := job.JobRuntime{Context: ctx}

		err := rules.Run(jobCtx)
		Expect(err).To(BeNil())

		var incidences []models.Incident
		err = db.Gorm.Where(&models.Incident{Description: incidentDescription}).Find(&incidences).Error
		Expect(err).To(BeNil())
		Expect(len(incidences)).To(Equal(2)) // There are 3 components but only 2 pass the filter.

		var incident *models.Incident
		err = db.Gorm.Where("title = ?", fmt.Sprintf("%s is %s", component.Name, component.Status)).First(&incident).Error
		Expect(err).To(BeNil())

		var anotherIncident *models.Incident
		err = db.Gorm.Where("title = ?", fmt.Sprintf("%s is %s", anotherComponent.Name, anotherComponent.Status)).First(&anotherIncident).Error
		Expect(err).To(BeNil())
	})
})
