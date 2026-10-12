package permissions_test

import (
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = Describe("Actions without a resource", Ordered, func() {
	const namespace = "resourceless-test"

	var operator, constrainedOperator, guest *models.Person

	uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }

	actions := map[string]string{
		adapter.ActionPropertyManage: adapter.ObjectProperties,
		adapter.ActionEventRead:      adapter.ObjectEvents,
		adapter.ActionJobRead:        adapter.ObjectJobs,
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
				{Name: "manage-properties", Action: adapter.ActionPropertyManage},
				{Name: "read-events", Action: adapter.ActionEventRead},
				{Name: "read-jobs", Action: adapter.ActionJobRead},
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

		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	AfterAll(func() {
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{}).Error).To(Succeed())
		for _, p := range []*models.Person{operator, constrainedOperator, guest} {
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
})
