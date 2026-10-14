package permissions_test

import (
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
)

// RoleBindings that select every person (specs/authorization/rolebindings.md, Section 2.1).
var _ = ginkgo.Describe("RoleBinding selecting every person", ginkgo.Ordered, func() {
	const namespace = "all-people"

	var (
		human, federated, agent, lateHuman *models.Person
		limitedToken, unlimitedToken       *models.Person
		tagged, other                      models.ConfigItem
		everyoneID                         = uuid.NewString()
	)

	allPeople := v1.RoleBindingSubjects{PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{"*"}}}

	allow := func(name, action, resource string) v1.RoleRule {
		return v1.RoleRule{Name: name, Action: action, Resource: &v1.ScopeReference{ScopeRef: resource}}
	}

	newScope := func(name string, targets ...v1.ScopeTarget) {
		ginkgo.GinkgoHelper()
		scope := &v1.Scope{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec:       v1.ScopeSpec{Targets: targets},
		}
		Expect(db.PersistScopeFromCRD(DefaultContext, scope)).To(Succeed())
	}

	newRole := func(name string, rules ...v1.RoleRule) {
		ginkgo.GinkgoHelper()
		role := &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec:       v1.RoleSpec{Rules: rules},
		}
		Expect(db.PersistRoleFromCRD(DefaultContext, role)).To(Succeed())
	}

	createToken := func(owner *models.Person, name string, roles ...string) *models.Person {
		ginkgo.GinkgoHelper()
		tokenRoles := lo.Map(roles, func(role string, _ int) models.AccessTokenRole {
			return models.AccessTokenRole{Namespace: namespace, Name: role}
		})
		result, err := db.CreateAccessTokenForPerson(DefaultContext, owner, name, 0, false, tokenRoles)
		Expect(err).ToNot(HaveOccurred())
		_, err = rbac.Enforcer().AddGroupingPolicy(result.Person.ID.String(), owner.ID.String())
		Expect(err).ToNot(HaveOccurred())
		return result.Person
	}

	canRead := func(subject *models.Person, config models.ConfigItem) bool {
		return mcRBAC.HasPermission(DefaultContext, subject.ID.String(), &models.ABACAttribute{Config: config}, policy.ActionRead)
	}

	assignedDirectly := func(subject *models.Person) bool {
		ginkgo.GinkgoHelper()
		assigned, err := rbac.Enforcer().HasRoleForUser(subject.ID.String(), models.BindingPrincipal(namespace, "everyone"))
		Expect(err).ToNot(HaveOccurred())
		return assigned
	}

	ginkgo.BeforeAll(func() {
		Expect(mcRBAC.Init(DefaultContext)).To(Succeed())

		human = setup.CreateUserWithRole(DefaultContext, "All People Human", "all-people-human@test.com")
		var err error
		federated, err = db.CreatePerson(DefaultContext, "All People Federated", "all-people-federated@test.com", db.PersonTypeFederated)
		Expect(err).ToNot(HaveOccurred())
		agent, err = db.CreatePerson(DefaultContext, "all-people-agent", "all-people-agent@local", db.PersonTypeAgent)
		Expect(err).ToNot(HaveOccurred())

		tagged = models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("all-people-tagged"), ConfigClass: "Pod", Type: lo.ToPtr("Kubernetes::Pod"), Tags: types.JSONStringMap{"all-people": "yes"}}
		other = models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("all-people-other"), ConfigClass: "Pod", Type: lo.ToPtr("Kubernetes::Pod"), Tags: types.JSONStringMap{"all-people": "no"}}
		for _, cfg := range []*models.ConfigItem{&tagged, &other} {
			Expect(DefaultContext.DB().Create(cfg).Error).To(Succeed())
		}

		newScope("tagged", v1.ScopeTarget{Config: &v1.ScopeConfigSelector{TagSelector: "all-people=yes"}})
		newScope("other", v1.ScopeTarget{Config: &v1.ScopeConfigSelector{TagSelector: "all-people=no"}})
		newRole("tagged-reader", allow("read", policy.ActionRead, "tagged"))
		newRole("other-reader", allow("read", policy.ActionRead, "other"))

		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "everyone", Namespace: namespace, UID: k8sTypes.UID(everyoneID)},
			Spec:       v1.RoleBindingSpec{Role: "tagged-reader", Subjects: allPeople},
		})).To(Succeed())

		limitedToken = createToken(human, "all-people-limited", "other-reader")
		unlimitedToken = createToken(human, "all-people-unlimited", "tagged-reader")
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	ginkgo.AfterAll(func() {
		for _, token := range []*models.Person{limitedToken, unlimitedToken} {
			DefaultContext.DB().Where("person_id = ?", token.ID).Delete(&models.AccessToken{})
			DefaultContext.DB().Delete(token)
		}
		for _, cfg := range []models.ConfigItem{tagged, other} {
			DefaultContext.DB().Delete(&cfg)
		}
		DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.RoleBinding{})
		DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{})
		DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{})
		for _, p := range []*models.Person{human, federated, agent, lateHuman} {
			if p != nil {
				DefaultContext.DB().Delete(p)
			}
		}
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	ginkgo.It("is Ready", func() {
		var binding models.RoleBinding
		Expect(DefaultContext.DB().Where("namespace = ? AND name = ?", namespace, "everyone").First(&binding).Error).To(Succeed())
		Expect(binding.Error).To(BeNil())
	})

	ginkgo.It("grants the Role to a person", func() {
		Expect(canRead(human, tagged)).To(BeTrue())
		Expect(canRead(human, other)).To(BeFalse())
	})

	ginkgo.It("doesn't grant the Role to agents or users of external identity providers", func() {
		Expect(canRead(agent, tagged)).To(BeFalse())
		Expect(canRead(federated, tagged)).To(BeFalse())
		Expect(assignedDirectly(agent)).To(BeFalse())
		Expect(assignedDirectly(federated)).To(BeFalse())
	})

	ginkgo.It("grants an access token the Role through its owner only, limited by the token's Roles", func() {
		Expect(assignedDirectly(limitedToken)).To(BeFalse())
		Expect(assignedDirectly(unlimitedToken)).To(BeFalse())

		Expect(canRead(unlimitedToken, tagged)).To(BeTrue())
		Expect(canRead(limitedToken, tagged)).To(BeFalse(), "its Roles don't allow it")
		Expect(canRead(limitedToken, other)).To(BeFalse(), "its owner isn't granted it")
	})

	ginkgo.It("grants the Role to a person created after the binding once the policy reloads", func() {
		lateHuman = setup.CreateUserWithRole(DefaultContext, "All People Late", "all-people-late@test.com")
		Expect(rbac.ReloadPolicy()).To(Succeed())
		Expect(canRead(lateHuman, tagged)).To(BeTrue())
	})

	ginkgo.It("stores nothing in casbin_rule", func() {
		var count int64
		Expect(DefaultContext.DB().Table("casbin_rule").Where("v1 = ?", models.BindingPrincipal(namespace, "everyone")).Count(&count).Error).To(Succeed())
		Expect(count).To(BeZero())
	})

	ginkgo.It("grants nothing once the binding is deleted", func() {
		Expect(db.DeleteRoleBinding(DefaultContext, everyoneID)).To(Succeed())
		Expect(rbac.ReloadPolicy()).To(Succeed())
		Expect(canRead(human, tagged)).To(BeFalse())
		Expect(assignedDirectly(human)).To(BeFalse())
	})
})
