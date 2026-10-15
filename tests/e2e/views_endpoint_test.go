package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// viewReader creates a person allowed to read every view through a legacy Permission.
func viewReader(name, email string) *models.Person {
	ginkgo.GinkgoHelper()
	person := setup.CreateUserWithRole(DefaultContext, name, email)
	perm := &v1.Permission{
		ObjectMeta: metav1.ObjectMeta{Name: "view-reader-" + person.ID.String(), Namespace: "default", UID: k8sTypes.UID(uuid.NewString())},
		Spec: v1.PermissionSpec{
			Subject: v1.PermissionSubject{Person: person.Email},
			Actions: []string{policy.ActionRead},
			Object: v1.PermissionObject{Selectors: dutyRBAC.Selectors{
				Views: []dutyRBAC.ViewRef{{Name: "*"}},
			}},
		},
	}
	Expect(db.PersistPermissionFromCRD(DefaultContext, perm)).To(Succeed())
	Expect(dutyRBAC.ReloadPolicy()).To(Succeed())
	ginkgo.DeferCleanup(func() {
		Expect(DefaultContext.DB().Delete(&models.Permission{}, "id = ?", string(perm.GetUID())).Error).To(Succeed())
	})
	return person
}

var _ = ginkgo.Describe("Views endpoint authorization", func() {
	ginkgo.It("should allow a view reader and deny a person without grants for GET /view/:id", func() {
		/*
			Context for this endpoint test:
			- viewer: reads the view through a Permission
			- guest: needs explicit permission to read views; without one, it is refused before the handler runs
		*/
		viewerUser := viewReader("Views Endpoint Viewer", "views-endpoint-viewer@test.com")
		guestUser := setup.CreateUserWithRole(DefaultContext, "Views Endpoint Guest", "views-endpoint-guest@test.com")

		ginkgo.DeferCleanup(func() {
			Expect(dutyRBAC.DeleteAllRolesForUser(viewerUser.ID.String())).To(Succeed())
			Expect(dutyRBAC.DeleteAllRolesForUser(guestUser.ID.String())).To(Succeed())
			Expect(DefaultContext.DB().Delete(&models.Person{}, "id IN ?", []string{viewerUser.ID.String(), guestUser.ID.String()}).Error).To(BeNil())
		})

		endpoint := fmt.Sprintf("%s/view/%s", server.URL, dummy.PodView.ID.String())

		viewerReq, err := http.NewRequest(http.MethodGet, endpoint, nil)
		Expect(err).ToNot(HaveOccurred())
		viewerReq.SetBasicAuth(viewerUser.Email, "test-password")

		viewerResp, err := http.DefaultClient.Do(viewerReq)
		Expect(err).ToNot(HaveOccurred())
		defer viewerResp.Body.Close()

		Expect(viewerResp.StatusCode).To(Equal(http.StatusOK))

		guestReq, err := http.NewRequest(http.MethodGet, endpoint, nil)
		Expect(err).ToNot(HaveOccurred())
		guestReq.SetBasicAuth(guestUser.Email, "test-password")

		guestResp, err := http.DefaultClient.Do(guestReq)
		Expect(err).ToNot(HaveOccurred())
		defer guestResp.Body.Close()

		Expect(guestResp.StatusCode).To(Equal(http.StatusForbidden))

		body, err := io.ReadAll(guestResp.Body)
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToLower(string(body))).To(ContainSubstring("access denied"))
	})

	ginkgo.It("should return metadata for GET /view/metadata/:id without rows", func() {
		viewerUser := viewReader("Views Metadata Viewer", "views-metadata-viewer@test.com")
		guestUser := setup.CreateUserWithRole(DefaultContext, "Views Metadata Guest", "views-metadata-guest@test.com")

		ginkgo.DeferCleanup(func() {
			Expect(dutyRBAC.DeleteAllRolesForUser(viewerUser.ID.String())).To(Succeed())
			Expect(dutyRBAC.DeleteAllRolesForUser(guestUser.ID.String())).To(Succeed())
			Expect(DefaultContext.DB().Delete(&models.Person{}, "id IN ?", []string{viewerUser.ID.String(), guestUser.ID.String()}).Error).To(BeNil())
		})

		endpoint := fmt.Sprintf("%s/view/metadata/%s", server.URL, dummy.PodView.ID.String())

		viewerReq, err := http.NewRequest(http.MethodGet, endpoint, nil)
		Expect(err).ToNot(HaveOccurred())
		viewerReq.SetBasicAuth(viewerUser.Email, "test-password")

		viewerResp, err := http.DefaultClient.Do(viewerReq)
		Expect(err).ToNot(HaveOccurred())
		defer viewerResp.Body.Close()

		Expect(viewerResp.StatusCode).To(Equal(http.StatusOK))

		var payload map[string]any
		Expect(json.NewDecoder(viewerResp.Body).Decode(&payload)).To(Succeed())
		Expect(payload).To(HaveKeyWithValue("id", dummy.PodView.ID.String()))
		Expect(payload).ToNot(HaveKey("rows"))

		guestReq, err := http.NewRequest(http.MethodGet, endpoint, nil)
		Expect(err).ToNot(HaveOccurred())
		guestReq.SetBasicAuth(guestUser.Email, "test-password")

		guestResp, err := http.DefaultClient.Do(guestReq)
		Expect(err).ToNot(HaveOccurred())
		defer guestResp.Body.Close()

		Expect(guestResp.StatusCode).To(Equal(http.StatusForbidden))

		body, err := io.ReadAll(guestResp.Body)
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToLower(string(body))).To(ContainSubstring("access denied"))
	})
})
