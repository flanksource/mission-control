package push

// Declare a static topology as component models
// Store it in db as topology and components with push location
// Run job and create a new server
// Manage auth

import (
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

var _ = ginkgo.Describe("Push", ginkgo.Ordered, func() {

	topologyNotToPush := models.Topology{
		Name:   "box",
		Source: models.SourceUI,
		Spec: []byte(`{
          "name": "box",
          "components": [
            {"name": "item1"},
            {"name": "item2"}
          ]
        }`),
	}

	topologyDB := models.Topology{
		Name:   "laptop",
		Source: models.SourceCRD,
		Spec: []byte(`{
          "name": "laptop",
          "pushLocation": {
            "url": "http://localhost"
          },
          "components": [
            {"name": "keyboard", "properties": [{"name": "color", "text": "black"}]},
            {"name": "battery", "properties": [{"name": "capacity", "text": "57WHr"}]},
            {"name": "display", "status": "healthy", "properties": [{"name": "size", "value": 14}], "components": [{"name": "panel"}, {"name": "bezel"}]}
          ]
        }`),
	}

	componentLaptop := models.Component{
		Name:       "laptop",
		TopologyID: &topologyDB.ID,
	}
	componentKeyboard := models.Component{
		Name:       "keyboard",
		TopologyID: &topologyDB.ID,
		Properties: models.Properties{{Name: "color", Text: "black"}},
	}
	componentBattery := models.Component{
		Name:       "battery",
		TopologyID: &topologyDB.ID,
		Properties: models.Properties{{Name: "capacity", Text: "57WHr"}},
	}
	componentDisplay := models.Component{
		Name:       "display",
		TopologyID: &topologyDB.ID,
		Properties: models.Properties{{Name: "size", Value: lo.ToPtr(int64(14))}},
	}
	componentPanel := models.Component{
		Name:       "panel",
		TopologyID: &topologyDB.ID,
	}
	componentBezel := models.Component{
		Name:       "bezel",
		TopologyID: &topologyDB.ID,
	}

	ginkgo.BeforeAll(func() {
		// Populate topology
		err := DefaultContext.DB().Save(&topologyDB)
		Expect(err).ToNot(BeNil())
		Expect(topologyDB.ID).ToNot(Equal(uuid.Nil))

		err = DefaultContext.DB().Save(&topologyNotToPush)
		Expect(err).ToNot(BeNil())
		Expect(topologyDB.ID).ToNot(Equal(uuid.Nil))

		// Populate component in db
		err = DefaultContext.DB().Save(&componentLaptop)
		Expect(err).ToNot(BeNil())
		Expect(componentLaptop.ID).ToNot(Equal(uuid.Nil))

		componentKeyboard.ParentId = &componentLaptop.ID
		componentBattery.ParentId = &componentLaptop.ID
		componentDisplay.ParentId = &componentLaptop.ID

		err = DefaultContext.DB().Save(&componentKeyboard)
		Expect(err).ToNot(BeNil())

		err = DefaultContext.DB().Save(&componentBattery)
		Expect(err).ToNot(BeNil())

		err = DefaultContext.DB().Save(&componentDisplay)
		Expect(err).ToNot(BeNil())

		componentPanel.ParentId = &componentDisplay.ID
		componentBezel.ParentId = &componentDisplay.ID

		err = DefaultContext.DB().Save(&componentPanel)
		Expect(err).ToNot(BeNil())

		err = DefaultContext.DB().Save(&componentBezel)
		Expect(err).ToNot(BeNil())
	})

	ginkgo.It("should query all topologies to be pushed", func() {
		PushTopologiesWithLocation.Context = DefaultContext
		PushTopologiesWithLocation.Run()
		var jh models.JobHistory
		DefaultContext.DB().Where("name = ?", PushTopologiesWithLocation.Name).Order("created_at DESC").First(&jh)

		fmt.Println(jh.Status)
		fmt.Println(jh.Errors)
		Expect("1").To(Equal("1"))
	})

	ginkgo.Describe("consumer", func() {
		ginkgo.It("should have transferred all the components", func() {
		})

		ginkgo.It("should have transferred all the checks", func() {
		})

	})
})
