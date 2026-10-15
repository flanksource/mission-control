package permissions_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
)

// A RoleBinding's constraint narrows what a person may approve, and an access review answers by the reviewed
// subject's own grants.
var _ = ginkgo.Describe("Approving a run and reviewing access", ginkgo.Ordered, func() {
	const namespace = "approve-and-review"

	var (
		e                       *echo.Echo
		admin, approver, reader *models.Person
		cfgIn, cfgOut           models.ConfigItem
		approvable              models.Playbook
	)

	ginkgo.BeforeAll(func() {
		Expect(mcRBAC.Init(DefaultContext)).To(Succeed())

		admin = newAdmin("Approve Review Admin", "approve-review-admin@test.com")
		approver = setup.CreateUserWithRole(DefaultContext, "Approve Review Approver", "approve-review-approver@test.com")
		reader = setup.CreateUserWithRole(DefaultContext, "Approve Review Reader", "approve-review-reader@test.com")

		for _, cfg := range []*models.ConfigItem{&cfgIn, &cfgOut} {
			side := lo.Ternary(cfg == &cfgIn, "in", "out")
			*cfg = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr(namespace + "-" + side),
				ConfigClass: "Pod",
				Type:        lo.ToPtr("Kubernetes::Pod"),
				Tags:        types.JSONStringMap{namespace: side},
			}
			Expect(DefaultContext.DB().Create(cfg).Error).To(Succeed())
		}

		approvable = models.Playbook{
			ID:        uuid.New(),
			Name:      namespace,
			Namespace: "default",
			Source:    models.SourceUI,
			Spec:      fmt.Appendf(nil, `{"configs":[{"name":"*"}],"approval":{"approvers":{"people":[%q]}},"actions":[{"name":"echo","exec":{"script":"echo"}}]}`, approver.Email),
		}
		Expect(DefaultContext.DB().Create(&approvable).Error).To(Succeed())

		uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }
		for name, targets := range map[string][]v1.ScopeTarget{
			"in": {
				{Config: &v1.ScopeConfigSelector{TagSelector: namespace + "=in"}},
				{Playbook: &v1.ScopePlaybookRef{Name: namespace}},
			},
			"configs":   {{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}}},
			"playbooks": {{Playbook: &v1.ScopePlaybookRef{Name: namespace}}},
		} {
			Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
				Spec:       v1.ScopeSpec{Targets: targets},
			})).To(Succeed())
		}

		// The approver may read every config and approve the playbook on any of them, narrowed by the binding to "in"
		Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: "approver", Namespace: namespace, UID: uid()},
			Spec: v1.RoleSpec{Rules: []v1.RoleRule{
				{Name: "read", Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: "configs"}},
				{
					Name:     "approve",
					Action:   policy.ActionPlaybookApprove,
					Resource: &v1.ScopeReference{ScopeRef: "playbooks"},
					Target:   &v1.ScopeReference{ScopeRef: "configs"},
				},
			}},
		})).To(Succeed())
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "approver", Namespace: namespace, UID: uid()},
			Spec: v1.RoleBindingSpec{
				Role:     "approver",
				Subjects: boundPeople(approver),
				Constraint: &v1.RoleBindingConstraint{
					Resource: &v1.ScopeReference{ScopeRef: "in"},
					Target:   &v1.ScopeReference{ScopeRef: "in"},
				},
			},
		})).To(Succeed())

		// The reader reads the configs in "in" only
		Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: "reader", Namespace: namespace, UID: uid()},
			Spec:       v1.RoleSpec{Rules: []v1.RoleRule{{Name: "read", Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: "in"}}}},
		})).To(Succeed())
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "reader", Namespace: namespace, UID: uid()},
			Spec:       v1.RoleBindingSpec{Role: "reader", Subjects: boundPeople(reader)},
		})).To(Succeed())

		Expect(rbac.ReloadPolicy()).To(Succeed())
		auth.FlushTokenCache()

		e = echo.New()
		playbook.RegisterRoutes(e)
		mcRBAC.RegisterRoutes(e)
	})

	ginkgo.AfterAll(func() {
		runs := DefaultContext.DB().Model(&models.PlaybookRun{}).Select("id").Where("playbook_id = ?", approvable.ID)
		Expect(DefaultContext.DB().Where("run_id IN (?)", runs).Delete(&models.PlaybookApproval{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("playbook_id = ?", approvable.ID).Delete(&models.PlaybookRun{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(&approvable).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(&cfgIn).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(&cfgOut).Error).To(Succeed())
		deleteNamespace(namespace)
		for _, person := range []*models.Person{admin, approver, reader} {
			Expect(DefaultContext.DB().Delete(person).Error).To(Succeed())
		}
	})

	do := func(person *models.Person, method, path string, body any) *httptest.ResponseRecorder {
		ginkgo.GinkgoHelper()
		var raw []byte
		if body != nil {
			var err error
			raw, err = json.Marshal(body)
			Expect(err).ToNot(HaveOccurred())
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req = req.WithContext(DefaultContext.WithUser(person))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	pendingRun := func(configID uuid.UUID) uuid.UUID {
		ginkgo.GinkgoHelper()
		run := models.PlaybookRun{
			ID:         uuid.New(),
			PlaybookID: approvable.ID,
			Status:     models.PlaybookRunStatusPendingApproval,
			Spec:       approvable.Spec,
			ConfigID:   &configID,
			CreatedBy:  &admin.ID,
		}
		Expect(DefaultContext.DB().Create(&run).Error).To(Succeed())
		return run.ID
	}

	ginkgo.Context("playbook:approve", func() {
		ginkgo.It("is allowed on a run against a config inside the binding's constraint", func() {
			rec := do(approver, http.MethodPost, "/playbook/run/approve/"+pendingRun(cfgIn.ID).String(), nil)
			Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		})

		ginkgo.It("is refused on a run against a config outside the binding's constraint", func() {
			rec := do(approver, http.MethodPost, "/playbook/run/approve/"+pendingRun(cfgOut.ID).String(), nil)
			Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())
		})
	})

	ginkgo.Context("an access review", func() {
		review := func(cfg models.ConfigItem) bool {
			ginkgo.GinkgoHelper()
			rec := do(admin, http.MethodPost, "/rbac/subject-access-reviews", map[string]any{
				"resource": map[string]any{"config": cfg.ID.String()},
				"action":   policy.ActionRead,
				"subjects": []string{reader.ID.String()},
			})
			Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())

			var response mcRBAC.SubjectAccessReviewResponse
			Expect(json.Unmarshal(rec.Body.Bytes(), &response)).To(Succeed())
			Expect(response.Results).To(HaveLen(1))
			return response.Results[0].Allowed
		}

		ginkgo.It("answers by the reviewed subject's grants", func() {
			// No action grants access reviews yet (specs/authorization/overview.md, "Not covered yet")
			_, err := rbac.Enforcer().AddPolicy(admin.ID.String(), policy.ObjectRBAC, policy.ActionRead, "allow", "", "access-review")
			Expect(err).ToNot(HaveOccurred())
			ginkgo.DeferCleanup(func() {
				_, err := rbac.Enforcer().RemovePolicy(admin.ID.String(), policy.ObjectRBAC, policy.ActionRead, "allow", "", "access-review")
				Expect(err).ToNot(HaveOccurred())
			})

			Expect(review(cfgIn)).To(BeTrue())
			Expect(review(cfgOut)).To(BeFalse(), "the reader has no grant on configs outside in")
		})
	})
})
