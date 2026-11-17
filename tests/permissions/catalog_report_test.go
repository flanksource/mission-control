package permissions_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/catalog"
	"github.com/flanksource/incident-commander/db"
	icrbac "github.com/flanksource/incident-commander/rbac"
)

var _ = Describe("Catalog report of configs the caller reads only some of", Ordered, func() {
	const namespace = "catalog-report-rls"

	var (
		e                      *echo.Echo
		configA, configB       models.ConfigItem
		reader, admin, deniedB *models.Person
		previewIDs             []string
	)

	preview := func(person *models.Person) (int, catalog.CatalogReportPreviewResponse) {
		GinkgoHelper()
		body, err := json.Marshal(catalog.CatalogReportRequest{SelectedIDs: previewIDs})
		Expect(err).ToNot(HaveOccurred())
		req := httptest.NewRequest(http.MethodPost, "/catalog/report/preview", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req = req.WithContext(DefaultContext.WithUser(person))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		var resp catalog.CatalogReportPreviewResponse
		if rec.Code == http.StatusOK {
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
		}
		return rec.Code, resp
	}

	BeforeAll(func() {
		Expect(icrbac.Init(DefaultContext)).To(Succeed())
		e = echo.New()
		catalog.RegisterRoutes(e)

		newConfig := func(name, tenant string) models.ConfigItem {
			return models.ConfigItem{
				ID: uuid.New(), Name: lo.ToPtr(name), ConfigClass: models.ConfigClassPod, Type: lo.ToPtr("Kubernetes::Pod"),
				Tags: types.JSONStringMap{"namespace": tenant},
			}
		}
		configA = newConfig("catalog-report-rls-a", "catalog-report-rls-tenant-a")
		configB = newConfig("catalog-report-rls-b", "catalog-report-rls-tenant-b")
		previewIDs = []string{configA.ID.String(), configB.ID.String()}
		// Scopes read stored membership, so the configs they select must exist before they're built
		Expect(DefaultContext.DB().Create(&configA).Error).To(Succeed())
		Expect(DefaultContext.DB().Create(&configB).Error).To(Succeed())

		reader = setup.CreateUserWithRole(DefaultContext, "Catalog Report Reader", "catalog-report-reader@test.com")
		admin = newAdmin("Catalog Report Admin", "catalog-report-admin@test.com")
		deniedB = newAdmin("Catalog Report Admin Denied B", "catalog-report-denied-b@test.com")

		uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }
		persistReadRole := func(name, tagSelector string) {
			Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
				Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{TagSelector: tagSelector}}}},
			})).To(Succeed())
			Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
				Spec:       v1.RoleSpec{Rules: []v1.RoleRule{{Name: "read", Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: name}}}},
			})).To(Succeed())
		}
		persistReadRole("tenant-a", "namespace=catalog-report-rls-tenant-a")
		persistReadRole("tenant-b", "namespace=catalog-report-rls-tenant-b")
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "reader", Namespace: namespace, UID: uid()},
			Spec:       v1.RoleBindingSpec{Role: "tenant-a", Subjects: boundPeople(reader)},
		})).To(Succeed())
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "deny-tenant-b", Namespace: namespace, UID: uid()},
			Spec:       v1.RoleBindingSpec{Role: "tenant-b", Subjects: boundPeople(deniedB), Deny: true},
		})).To(Succeed())
		Expect(rbac.ReloadPolicy()).To(Succeed())
		auth.FlushTokenCache()
	})

	AfterAll(func() {
		deleteNamespace(namespace)
		for _, row := range []any{&configA, &configB, reader, admin, deniedB} {
			Expect(DefaultContext.DB().Delete(row).Error).To(Succeed())
		}
	})

	It("previews every selected config for the admin", func() {
		code, resp := preview(admin)
		Expect(code).To(Equal(http.StatusOK))
		Expect(resp.SelectedIDs).To(ConsistOf(previewIDs))
	})

	It("refuses a reader of some of the catalog, even after the admin read the configs", func() {
		code, _ := preview(reader)
		Expect(code).To(Equal(http.StatusForbidden))
	})

	It("refuses an admin denied part of the catalog", func() {
		code, _ := preview(deniedB)
		Expect(code).To(Equal(http.StatusForbidden))
	})
})
