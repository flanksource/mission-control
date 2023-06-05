package main

import (
	"fmt"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rules"
	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Test incident creation via incidence rule", ginkgo.Ordered, func() {
	var (
		incidentRule *api.IncidentRule
		component    *models.Component
	)

	ginkgo.It("should create a system user", func() {
		systemUser := &models.Person{
			ID:   uuid.New(),
			Name: "System",
		}
		tx := db.Gorm.Create(systemUser)
		Expect(tx.Error).To(BeNil())

		api.SystemUserID = &systemUser.ID
	})

	ginkgo.It("should create a new component", func() {
		component = &models.Component{
			ID:         uuid.New(),
			Name:       "Component For Rule",
			Type:       "Entity",
			ExternalId: "dummy/logistics",
		}
		tx := db.Gorm.Create(component)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create an incidence rule", func() {
		incidentRule = &api.IncidentRule{
			ID:   uuid.New(),
			Name: "My incident rule",
			Spec: &api.IncidentRuleSpec{
				Name:       "what is this name",
				Components: []api.ComponentSelector{{Name: component.Name}},
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

	ginkgo.It("should mark the component as unhealthy", func() {
		component.Status = "unhealthy"
		tx := db.Gorm.Save(component)
		Expect(tx.Error).To(BeNil())
	})

	ginkgo.It("should create an incidence", func() {
		err := rules.Run()
		Expect(err).To(BeNil())

		var incidence *models.Incident
		err = db.Gorm.Where("title = ?", fmt.Sprintf("%s is %s", component.Name, component.Status)).First(&incidence).Error
		Expect(err).To(BeNil())
	})
})
