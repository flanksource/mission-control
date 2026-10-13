package permissions_test

import (
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = Describe("Actions without a resource", Ordered, func() {
	const namespace = "resourceless-test"

	var operator, constrainedOperator, guest *models.Person

	// holders has a person per action, granted only that action
	holders := map[string]*models.Person{}

	uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }

	actions := map[string]string{
		adapter.ActionPersonManage:  policy.ObjectPeople,
		policy.ActionMCPUse:         policy.ObjectMCP,
		policy.ActionPropertyManage: policy.ObjectProperties,
		policy.ActionEventRead:      policy.ObjectEvents,
		policy.ActionJobRead:        policy.ObjectJobs,
	}

	var binding, constrainedBinding *v1.RoleBinding

	BeforeAll(func() {
		Expect(rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)).To(Succeed())

		operator = setup.CreateUserWithRole(DefaultContext, "Resourceless Operator", "resourceless-operator@test.com", policy.RoleGuest)
		constrainedOperator = setup.CreateUserWithRole(DefaultContext, "Resourceless Constrained", "resourceless-constrained@test.com", policy.RoleGuest)
		guest = setup.CreateUserWithRole(DefaultContext, "Resourceless Guest", "resourceless-guest@test.com", policy.RoleGuest)

		Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{
			ObjectMeta: metav1.ObjectMeta{Name: "all-configs", Namespace: namespace, UID: uid()},
			Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}}}},
		})).To(Succeed())

		Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: "operations", Namespace: namespace, UID: uid()},
			Spec: v1.RoleSpec{Rules: []v1.RoleRule{
				{Name: "manage-people", Action: adapter.ActionPersonManage},
				{Name: "use-mcp", Action: policy.ActionMCPUse},
				{Name: "manage-properties", Action: policy.ActionPropertyManage},
				{Name: "read-events", Action: policy.ActionEventRead},
				{Name: "read-jobs", Action: policy.ActionJobRead},
				{Name: "read-configs", Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: "all-configs"}},
			}},
		})).To(Succeed())

		binding = &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "operators", Namespace: namespace, UID: uid()},
			Spec: v1.RoleBindingSpec{Role: "operations", Subjects: v1.RoleBindingSubjects{
				PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{operator.Email}},
			}},
		}
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).To(Succeed())

		constrainedBinding = &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "constrained-operators", Namespace: namespace, UID: uid()},
			Spec: v1.RoleBindingSpec{
				Role:       "operations",
				Subjects:   v1.RoleBindingSubjects{PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{constrainedOperator.Email}}},
				Constraint: &v1.RoleBindingConstraint{Resource: &v1.ScopeReference{ScopeRef: "all-configs"}},
			},
		}
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, constrainedBinding)).To(Succeed())

		for action, object := range actions {
			holder := setup.CreateUserWithRole(DefaultContext, "Resourceless "+object, "resourceless-"+object+"@test.com", policy.RoleGuest)
			holders[action] = holder
			Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: "only-" + object, Namespace: namespace, UID: uid()},
				Spec:       v1.RoleSpec{Rules: []v1.RoleRule{{Name: "only", Action: action}}},
			})).To(Succeed())
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "only-" + object, Namespace: namespace, UID: uid()},
				Spec: v1.RoleBindingSpec{Role: "only-" + object, Subjects: v1.RoleBindingSubjects{
					PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{holder.Email}},
				}},
			})).To(Succeed())
		}

		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	AfterAll(func() {
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{}).Error).To(Succeed())
		for _, p := range append([]*models.Person{operator, constrainedOperator, guest}, lo.Values(holders)...) {
			Expect(DefaultContext.DB().Delete(p).Error).To(Succeed())
		}
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	for action, object := range actions {
		It("grants "+action+" through a binding", func() {
			Expect(rbac.Check(DefaultContext, operator.ID.String(), object, action)).To(BeTrue())
		})

		It("doesn't grant "+action+" on any other object", func() {
			Expect(rbac.Check(DefaultContext, operator.ID.String(), policy.ObjectMonitor, action)).To(BeFalse())
			Expect(rbac.Check(DefaultContext, operator.ID.String(), object, policy.ActionRead)).To(BeFalse())
		})

		It("doesn't grant "+action+" without a binding", func() {
			Expect(rbac.Check(DefaultContext, guest.ID.String(), object, action)).To(BeFalse())
		})

		It("never grants "+action+" through a binding with a constraint", func() {
			Expect(rbac.Check(DefaultContext, constrainedOperator.ID.String(), object, action)).To(BeFalse())
		})

		It("grants "+action+" to admins", func() {
			Expect(rbac.Check(DefaultContext, "admin", object, action)).To(BeTrue())
		})
	}

	It("reports the rules a constraint can't narrow", func() {
		condition := meta.FindStatusCondition(constrainedBinding.Status.Conditions, v1.ConditionAllRulesApply)
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(adapter.ReasonConstraintDoesNotFit))
		Expect(condition.Message).To(ContainSubstring("takes no resource"))

		Expect(meta.IsStatusConditionTrue(binding.Status.Conditions, v1.ConditionAllRulesApply)).To(BeTrue())
	})

	Describe("through /db", func() {
		status := func(person *models.Person, method, table string) int {
			GinkgoHelper()
			req := httptest.NewRequest(method, "/db/"+table, nil)
			req = req.WithContext(DefaultContext.WithUser(person))
			rec := httptest.NewRecorder()
			handler := mcRBAC.DbMiddleware()(func(c echo.Context) error { return c.NoContent(http.StatusOK) })
			Expect(handler(echo.New().NewContext(req, rec))).To(Succeed())
			return rec.Code
		}

		readOnly := map[string][]string{
			policy.ActionJobRead:   {"job_history", "job_histories", "job_history_names", "job_history_summary", "job_history_latest_status"},
			policy.ActionEventRead: {"event_queue", "event_queue_summary", "failed_events"},
		}

		for action, tables := range readOnly {
			for _, table := range tables {
				It("lets a subject with only "+action+" read "+table, func() {
					Expect(status(holders[action], http.MethodGet, table)).To(Equal(http.StatusOK))
				})

				It("refuses writes to "+table+", even to a subject with "+action, func() {
					for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
						Expect(status(holders[action], method, table)).To(Equal(http.StatusMethodNotAllowed), method)
					}
				})

				It("refuses reading "+table+" without "+action, func() {
					Expect(status(guest, http.MethodGet, table)).To(Equal(http.StatusForbidden))
					for other, holder := range holders {
						if other != action {
							Expect(status(holder, http.MethodGet, table)).To(Equal(http.StatusForbidden), other)
						}
					}
				})
			}
		}

		It("lets every signed-in subject read properties", func() {
			Expect(status(guest, http.MethodGet, "properties")).To(Equal(http.StatusOK))
			Expect(status(holders[policy.ActionPropertyManage], http.MethodGet, "properties")).To(Equal(http.StatusOK))
		})

		It("lets a subject with property:manage write properties", func() {
			for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
				Expect(status(holders[policy.ActionPropertyManage], method, "properties")).To(Equal(http.StatusOK), method)
			}
		})

		It("refuses writing properties without property:manage", func() {
			for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
				Expect(status(guest, method, "properties")).To(Equal(http.StatusForbidden), method)
				Expect(status(holders[policy.ActionJobRead], method, "properties")).To(Equal(http.StatusForbidden), method)
			}
		})
	})
})
