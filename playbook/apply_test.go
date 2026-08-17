package playbook

import (
	"encoding/json"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/clientapi"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Apply playbook", func() {
	ginkgo.It("validates and persists API-owned playbooks", func() {
		name := "clientapi-" + uuid.NewString()
		defer DefaultContext.DB().Unscoped().Where("name = ?", name).Delete(&models.Playbook{})

		response, err := applyPlaybook(DefaultContext, clientapi.PlaybookApplyRequest{
			Namespace: "default",
			Name:      name,
			Spec:      json.RawMessage(`{"title":"Client API","actions":[{"name":"echo","exec":{"script":"echo ok"}}]}`),
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(response.Created).To(BeTrue())
		Expect(response.Playbook.Name).To(Equal(name))
		Expect(response.Playbook.Title).To(Equal("Client API"))
		Expect(response.Playbook.Source).To(Equal(models.SourceUI))
	})

	ginkgo.It("rejects invalid specs before persistence", func() {
		_, err := applyPlaybook(DefaultContext, clientapi.PlaybookApplyRequest{
			Namespace: "default",
			Name:      "invalid-" + uuid.NewString(),
			Spec:      json.RawMessage(`{"actions":[]}`),
		})

		Expect(err).To(HaveOccurred())
	})
})
