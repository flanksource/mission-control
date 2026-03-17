package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Views endpoint authorization", func() {
	ginkgo.It("should allow viewer role and deny guest role for GET /view/:id", func() {
		/*
			Context for this endpoint test:
			- viewer: gets permission by default from built-in RBAC policies for views:read
			- guest: needs explicit permission to read views
		*/
		viewerUser := setup.CreateUserWithRole(DefaultContext, "Views Endpoint Viewer", "views-endpoint-viewer@test.com", policy.RoleViewer)
		guestUser := setup.CreateUserWithRole(DefaultContext, "Views Endpoint Guest", "views-endpoint-guest@test.com", policy.RoleGuest)

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
		Expect(string(body)).To(ContainSubstring("access denied"))
	})

	ginkgo.It("should return metadata for GET /view/metadata/:id without rows", func() {
		viewerUser := setup.CreateUserWithRole(DefaultContext, "Views Metadata Viewer", "views-metadata-viewer@test.com", policy.RoleViewer)
		guestUser := setup.CreateUserWithRole(DefaultContext, "Views Metadata Guest", "views-metadata-guest@test.com", policy.RoleGuest)

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
		Expect(string(body)).To(ContainSubstring("access denied"))
	})
})
