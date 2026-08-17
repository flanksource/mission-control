package playbook

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/incident-commander/clientapi"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Apply playbook", func() {
	ginkgo.It("validates and persists API-owned playbooks", func() {
		name := "clientapi-" + uuid.NewString()
		defer DefaultContext.DB().Unscoped().Where("name = ?", name).Delete(&models.Playbook{})
		var authorizedAction string

		response, err := applyPlaybook(DefaultContext, clientapi.PlaybookApplyRequest{
			Namespace: "default",
			Name:      name,
			Spec:      json.RawMessage(`{"title":"Client API","actions":[{"name":"echo","exec":{"script":"echo ok"}}]}`),
		}, func(action string) error {
			authorizedAction = action
			return nil
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(authorizedAction).To(Equal(policy.ActionCreate))
		Expect(response.Created).To(BeTrue())
		Expect(response.Playbook.Name).To(Equal(name))
		Expect(response.Playbook.Title).To(Equal("Client API"))
		Expect(response.Playbook.Source).To(Equal(models.SourceUI))

		response, err = applyPlaybook(DefaultContext, clientapi.PlaybookApplyRequest{
			Namespace: "default",
			Name:      name,
			Spec:      json.RawMessage(`{"title":"Updated Client API","actions":[{"name":"echo","exec":{"script":"echo ok"}}]}`),
		}, func(action string) error {
			authorizedAction = action
			return nil
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(authorizedAction).To(Equal(policy.ActionUpdate))
		Expect(response.Created).To(BeFalse())
		Expect(response.Playbook.Title).To(Equal("Updated Client API"))
	})

	ginkgo.It("rejects invalid specs before persistence", func() {
		_, err := applyPlaybook(DefaultContext, clientapi.PlaybookApplyRequest{
			Namespace: "default",
			Name:      "invalid-" + uuid.NewString(),
			Spec:      json.RawMessage(`{"actions":[]}`),
		}, func(string) error { return nil })

		Expect(err).To(HaveOccurred())
	})

	ginkgo.It("rejects webhook path collisions on create and update", func() {
		namePrefix := "clientapi-webhook-" + uuid.NewString()
		defer DefaultContext.DB().Unscoped().Where("name LIKE ?", namePrefix+"%").Delete(&models.Playbook{})
		authorize := func(string) error { return nil }
		spec := func(title, path string) json.RawMessage {
			return json.RawMessage(fmt.Sprintf(`{"title":%q,"on":{"webhook":{"path":%q}},"actions":[{"name":"echo","exec":{"script":"echo ok"}}]}`, title, path))
		}

		_, err := applyPlaybook(DefaultContext, clientapi.PlaybookApplyRequest{
			Namespace: "default",
			Name:      namePrefix + "-first",
			Spec:      spec("First", namePrefix+"-first"),
		}, authorize)
		Expect(err).ToNot(HaveOccurred())

		_, err = applyPlaybook(DefaultContext, clientapi.PlaybookApplyRequest{
			Namespace: "default",
			Name:      namePrefix + "-second",
			Spec:      spec("Second", namePrefix+"-second"),
		}, authorize)
		Expect(err).ToNot(HaveOccurred())

		_, err = applyPlaybook(DefaultContext, clientapi.PlaybookApplyRequest{
			Namespace: "default",
			Name:      namePrefix + "-third",
			Spec:      spec("Third", namePrefix+"-first"),
		}, authorize)
		Expect(err).To(MatchError(ContainSubstring("webhook path")))

		_, err = applyPlaybook(DefaultContext, clientapi.PlaybookApplyRequest{
			Namespace: "default",
			Name:      namePrefix + "-second",
			Spec:      spec("Second", namePrefix+"-first"),
		}, authorize)
		Expect(err).To(MatchError(ContainSubstring("webhook path")))
	})
})
