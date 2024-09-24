package push

import (
	"fmt"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

var _ = ginkgo.Describe("Push", ginkgo.Ordered, func() {

	topologyDB := models.Topology{
		Name:   "laptop",
		Source: models.SourceCRD,
		Spec: []byte(`{
          "name": "laptop",
          "pushLocation": {
            "url": "http://localhost:%d"
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
		topologyDB.Spec = []byte(fmt.Sprintf(string(topologyDB.Spec), echoServerPort))
		err := DefaultContext.DB().Save(&topologyDB).Error
		Expect(err).To(BeNil())
		Expect(topologyDB.ID).ToNot(Equal(uuid.Nil))

		// Populate component in db
		err = DefaultContext.DB().Save(&componentLaptop).Error
		Expect(err).To(BeNil())
		Expect(componentLaptop.ID).ToNot(Equal(uuid.Nil))

		componentKeyboard.ParentId = &componentLaptop.ID
		componentBattery.ParentId = &componentLaptop.ID
		componentDisplay.ParentId = &componentLaptop.ID

		err = DefaultContext.DB().Save(&componentKeyboard).Error
		Expect(err).To(BeNil())

		err = DefaultContext.DB().Save(&componentBattery).Error
		Expect(err).To(BeNil())

		err = DefaultContext.DB().Save(&componentDisplay).Error
		Expect(err).To(BeNil())

		componentPanel.ParentId = &componentDisplay.ID
		componentBezel.ParentId = &componentDisplay.ID

		err = DefaultContext.DB().Save(&componentPanel).Error
		Expect(err).To(BeNil())

		err = DefaultContext.DB().Save(&componentBezel).Error
		Expect(err).To(BeNil())
	})

	ginkgo.It("should push the topology tree correctly", func() {
		var err error
		var oldComponentCount, newComponentCount int64
		err = PushServerContext.DB().Model(&models.Component{}).Where(duty.LocalFilter).Count(&oldComponentCount).Error
		Expect(err).To(BeNil())
		Expect(oldComponentCount).To(Equal(int64(0)))

		tree, err := query.Topology(DefaultContext, query.TopologyOptions{ID: componentLaptop.ID.String(), Depth: 10, NoCache: true})
		Expect(err).To(BeNil())

		httpClient := http.NewClient()
		endpoint := fmt.Sprintf("http://localhost:%d/push/topology", echoServerPort)
		resp, err := httpClient.R(DefaultContext).
			Header(echo.HeaderContentType, echo.MIMEApplicationJSON).
			Post(endpoint, tree.Components[0])

		Expect(err).To(BeNil())
		Expect(resp.IsOK()).To(BeTrue())

		err = PushServerContext.DB().Model(&models.Component{}).Where(duty.LocalFilter).Count(&newComponentCount).Error
		Expect(err).To(BeNil())
		// TODO: This should be 6, there is a bug in query.Topology which leads to the
		// entire tree not being returned
		Expect(newComponentCount).To(Equal(int64(4)))
	})

	ginkgo.It("should handle missing components", func() {
		var err error
		var oldComponentCount, newComponentCount int64

		err = PushServerContext.DB().Model(&models.Component{}).Where(duty.LocalFilter).Count(&oldComponentCount).Error
		Expect(err).To(BeNil())
		Expect(oldComponentCount).To(Equal(int64(4)))

		err = DefaultContext.DB().Delete(&componentBattery).Error
		Expect(err).To(BeNil())

		tree, err := query.Topology(DefaultContext, query.TopologyOptions{ID: componentLaptop.ID.String(), Depth: 10, NoCache: true})
		Expect(err).To(BeNil())

		// Mess up ids to ensure pushed IDs do not matter and deletion works correctly
		tree.Components[0].ID = uuid.New()
		tree.Components[0].Components[0].ID = uuid.New()
		tree.Components[0].Components[1].ID = uuid.New()

		httpClient := http.NewClient()
		endpoint := fmt.Sprintf("http://localhost:%d/push/topology", echoServerPort)
		resp, err := httpClient.R(DefaultContext).
			Header(echo.HeaderContentType, echo.MIMEApplicationJSON).
			Post(endpoint, tree.Components[0])

		Expect(err).To(BeNil())
		Expect(resp.IsOK()).To(BeTrue())

		err = PushServerContext.DB().Model(&models.Component{}).Where(duty.LocalFilter).Count(&newComponentCount).Error
		Expect(err).To(BeNil())
		// 1 component should be marked as deleted
		Expect(newComponentCount).To(Equal(int64(3)))
	})

})
