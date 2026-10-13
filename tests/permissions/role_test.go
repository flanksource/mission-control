package permissions_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/kopper"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/catalog"
	"github.com/flanksource/incident-commander/db"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = Describe("Role and RoleBinding", Ordered, func() {
	const (
		namespace      = "role-test"
		otherNamespace = "role-other"
	)

	var (
		alice, bob, carol, dave, erin, builtinAgent, rlsGuest, rlsTenantGuest *models.Person
		teamA, teamB                                                          *models.Team
		roles                                                                 = map[string]*v1.Role{}
	)

	newScope := func(name, ns string, targets ...v1.ScopeTarget) *v1.Scope {
		return &v1.Scope{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, UID: k8sTypes.UID(uuid.NewString())},
			Spec:       v1.ScopeSpec{Targets: targets},
		}
	}

	allow := func(name, action, resource string) v1.RoleRule {
		return v1.RoleRule{Name: name, Action: action, Resource: &v1.ScopeReference{ScopeRef: resource}}
	}

	on := func(rule v1.RoleRule, target string) v1.RoleRule {
		rule.Target = &v1.ScopeReference{ScopeRef: target}
		return rule
	}

	deny := func(rule v1.RoleRule) v1.RoleRule {
		rule.Deny = true
		return rule
	}

	newRole := func(name, ns string, rules ...v1.RoleRule) *v1.Role {
		return &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, UID: k8sTypes.UID(uuid.NewString())},
			Spec:       v1.RoleSpec{Rules: rules},
		}
	}

	newBinding := func(name, role string, subjects v1.RoleBindingSubjects) *v1.RoleBinding {
		return &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec:       v1.RoleBindingSpec{Role: role, Subjects: subjects},
		}
	}

	constrained := func(binding *v1.RoleBinding, resource, target string) *v1.RoleBinding {
		c := &v1.RoleBindingConstraint{}
		if resource != "" {
			c.Resource = &v1.ScopeReference{ScopeRef: resource}
		}
		if target != "" {
			c.Target = &v1.ScopeReference{ScopeRef: target}
		}
		binding.Spec.Constraint = c
		return binding
	}

	people := func(p ...*models.Person) v1.RoleBindingSubjects {
		return v1.RoleBindingSubjects{PermissionGroupSubjects: v1.PermissionGroupSubjects{
			People: lo.Map(p, func(person *models.Person, _ int) string { return person.Email }),
		}}
	}

	configTagged := func(tags map[string]string) models.ConfigItem {
		return models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("config"), Type: lo.ToPtr("Role::Test"), ConfigClass: "Test", Tags: tags}
	}

	var (
		tenantA = configTagged(map[string]string{"tenant": "a", "env": "role-test"})
		tenantB = configTagged(map[string]string{"tenant": "b", "env": "role-test"})
	)

	canRunOn := func(subject string, playbook models.Playbook, config *models.ConfigItem) bool {
		attr := &models.ABACAttribute{Playbook: playbook}
		if config != nil {
			attr.Config = *config
		}
		return rbac.HasPermission(DefaultContext, subject, attr, policy.ActionPlaybookRun)
	}

	canRead := func(subject string, config models.ConfigItem) bool {
		return rbac.HasPermission(DefaultContext, subject, &models.ABACAttribute{Config: config}, policy.ActionRead)
	}

	persistRole := func(role *v1.Role) {
		GinkgoHelper()
		Expect(db.PersistRoleFromCRD(DefaultContext, role)).To(Succeed())
		roles[role.Namespace+"/"+role.Name] = role
	}

	BeforeAll(func() {
		Expect(mcRBAC.Init(DefaultContext)).To(Succeed())

		alice = setup.CreateUserWithRole(DefaultContext, "Role Alice", "role-alice@test.com")
		bob = setup.CreateUserWithRole(DefaultContext, "Role Bob", "role-bob@test.com")
		carol = setup.CreateUserWithRole(DefaultContext, "Role Carol", "role-carol@test.com")
		dave = setup.CreateUserWithRole(DefaultContext, "Role Dave", "role-dave@test.com")
		erin = setup.CreateUserWithRole(DefaultContext, "Role Erin", "role-erin@test.com")
		builtinAgent = setup.CreateUserWithRole(DefaultContext, "Role Agent", "role-agent@test.com", policy.RoleAgent)
		rlsGuest = setup.CreateUserWithRole(DefaultContext, "Role RLS Guest", "role-rls@test.com")
		rlsTenantGuest = setup.CreateUserWithRole(DefaultContext, "Role RLS Tenant Guest", "role-rls-tenant@test.com")

		teamA = &models.Team{Name: "role-team-a", CreatedBy: alice.ID}
		teamB = &models.Team{Name: "role-team-b", CreatedBy: alice.ID}
		Expect(DefaultContext.DB().Create(teamA).Error).To(Succeed())
		Expect(DefaultContext.DB().Create(teamB).Error).To(Succeed())

		// Checks read stored membership, so the configs they're made on must exist before the Scopes are built
		for _, config := range []models.ConfigItem{tenantA, tenantB} {
			Expect(DefaultContext.DB().Create(&config).Error).To(Succeed())
		}

		for _, scope := range []*v1.Scope{
			newScope("all-playbooks", namespace, v1.ScopeTarget{Playbook: &v1.ScopePlaybookRef{Name: "*"}}),
			newScope("all-playbooks", otherNamespace, v1.ScopeTarget{Playbook: &v1.ScopePlaybookRef{Name: "*"}}),
			newScope("kubernetes-playbooks", namespace, v1.ScopeTarget{Playbook: &v1.ScopePlaybookRef{Name: dummy.RestartPod.Name}}),
			newScope("all-configs", namespace, v1.ScopeTarget{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}}),
			newScope("tenant-a", namespace, v1.ScopeTarget{Config: &v1.ScopeConfigSelector{TagSelector: "tenant=a"}}),
			newScope("tenant-b", namespace, v1.ScopeTarget{Config: &v1.ScopeConfigSelector{TagSelector: "tenant=b"}}),
			newScope("tenant-a-boundary", namespace,
				v1.ScopeTarget{Config: &v1.ScopeConfigSelector{TagSelector: "tenant=a"}},
				v1.ScopeTarget{Playbook: &v1.ScopePlaybookRef{Name: "*"}}),
			newScope("role-configs", namespace, v1.ScopeTarget{Config: &v1.ScopeConfigSelector{TagSelector: "env=role-test"}}),
			newScope("staging-views", namespace, v1.ScopeTarget{View: &v1.ScopeViewRef{Namespace: "staging"}}),
			newScope("configs-and-playbooks", namespace,
				v1.ScopeTarget{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}},
				v1.ScopeTarget{Playbook: &v1.ScopePlaybookRef{Name: "*"}}),
			newScope("staging-components", namespace, v1.ScopeTarget{Component: &v1.ScopeLabelledSelector{ScopeResourceRef: v1.ScopeResourceRef{Namespace: "staging"}}}),
			newScope("http-checks", namespace, v1.ScopeTarget{Check: &v1.ScopeLabelledSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "http"}}}),
		} {
			Expect(db.PersistScopeFromCRD(DefaultContext, scope)).To(Succeed())
		}

		for _, role := range []*v1.Role{
			newRole("run-playbooks", namespace, allow("run-without-target", policy.ActionPlaybookRun, "all-playbooks")),
			newRole("run-anywhere", namespace, on(allow("run", policy.ActionPlaybookRun, "all-playbooks"), "all-configs")),
			newRole("run-kubernetes", namespace, on(allow("run", policy.ActionPlaybookRun, "kubernetes-playbooks"), "all-configs")),
			newRole("read-configs", namespace,
				allow("read", policy.ActionRead, "role-configs"),
				on(allow("run", policy.ActionPlaybookRun, "kubernetes-playbooks"), "all-configs")),
			newRole("tenant-a-runner", namespace, on(allow("run", policy.ActionPlaybookRun, "kubernetes-playbooks"), "tenant-a")),
			newRole("tenant-b-runner", namespace, on(allow("run", policy.ActionPlaybookRun, "all-playbooks"), "tenant-b")),
			newRole("only-in-other-namespace", otherNamespace, allow("run", policy.ActionPlaybookRun, "all-playbooks")),
		} {
			persistRole(role)
		}

		for _, b := range []*v1.RoleBinding{
			newBinding("people-and-teams", "run-playbooks", v1.RoleBindingSubjects{
				PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{alice.Email}, Teams: []string{teamA.Name, teamB.Name}},
			}),
			newBinding("pairings-a", "tenant-a-runner", people(bob)),
			newBinding("pairings-b", "tenant-b-runner", people(bob)),
			newBinding("run-anywhere", "run-anywhere", people(carol)),
			constrained(newBinding("tenant-a-operators", "read-configs", people(dave, rlsTenantGuest)), "tenant-a-boundary", "tenant-a-boundary"),
			newBinding("rls", "read-configs", people(rlsGuest)),
		} {
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, b)).To(Succeed())
		}

		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	AfterAll(func() {
		namespaces := []string{namespace, otherNamespace}
		Expect(DefaultContext.DB().Where("namespace IN ?", namespaces).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace IN ?", namespaces).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace IN ?", namespaces).Delete(&models.Scope{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("id IN ?", []uuid.UUID{tenantA.ID, tenantB.ID}).Delete(&models.ConfigItem{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(teamA).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(teamB).Error).To(Succeed())
		for _, p := range []*models.Person{alice, bob, carol, dave, erin, builtinAgent, rlsGuest, rlsTenantGuest} {
			Expect(DefaultContext.DB().Delete(p).Error).To(Succeed())
		}
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	storedRole := func(uid k8sTypes.UID) models.Role {
		GinkgoHelper()
		var role models.Role
		Expect(DefaultContext.DB().Where("id = ?", uid).First(&role).Error).To(Succeed())
		return role
	}

	storedBinding := func(ns, name string) models.RoleBinding {
		GinkgoHelper()
		var binding models.RoleBinding
		Expect(DefaultContext.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", ns, name).First(&binding).Error).To(Succeed())
		return binding
	}

	It("doesn't store its rules as permissions", func() {
		var count int64
		Expect(DefaultContext.DB().Model(&models.Permission{}).Where("subject LIKE ? OR subject LIKE ?", "role:%", "binding:%").Count(&count).Error).To(Succeed())
		Expect(count).To(BeZero())
	})

	Describe("subjects", func() {
		It("grants the role to people", func() {
			Expect(canRunOn(alice.ID.String(), dummy.EchoConfig, nil)).To(BeTrue())
		})

		It("grants the role to every listed team", func() {
			for _, team := range []*models.Team{teamA, teamB} {
				hasRole, err := rbac.Enforcer().HasRoleForUser(team.ID.String(), models.BindingPrincipal(namespace, "people-and-teams"))
				Expect(err).ToNot(HaveOccurred())
				Expect(hasRole).To(BeTrue(), team.Name)
			}
		})

		It("rejects a binding that selects built-in roles, which grant nothing", func() {
			err := db.PersistRoleBindingFromCRD(DefaultContext, newBinding("built-in-roles", "run-playbooks", v1.RoleBindingSubjects{Roles: []string{policy.RoleAgent}}))
			Expect(err).To(MatchError(ContainSubstring("subjects.roles is no longer supported")))
			Expect(rbac.ReloadPolicy()).To(Succeed())
			Expect(canRunOn(builtinAgent.ID.String(), dummy.EchoConfig, nil)).To(BeFalse())
		})

		It("grants nothing to subjects without a binding", func() {
			Expect(canRunOn(uuid.NewString(), dummy.EchoConfig, nil)).To(BeFalse())
		})

		It("keeps bindings out of everyone", func() {
			principal := models.BindingPrincipal(namespace, "people-and-teams")
			Expect(rbac.Check(DefaultContext, principal, policy.ObjectCatalog, policy.ActionRead)).To(BeFalse())

			hasEveryone, err := rbac.Enforcer().HasRoleForUser(principal, policy.RoleEveryone)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasEveryone).To(BeFalse())
		})
	})

	Describe("targets", func() {
		It("matches a rule without a target only for operations without a target", func() {
			Expect(canRunOn(alice.ID.String(), dummy.EchoConfig, nil)).To(BeTrue())
			Expect(canRunOn(alice.ID.String(), dummy.EchoConfig, &tenantA)).To(BeFalse())
		})

		It("matches a rule with a target only for operations on a target in its scope", func() {
			subject := carol.ID.String()
			Expect(canRunOn(subject, dummy.EchoConfig, &tenantA)).To(BeTrue())
			Expect(canRunOn(subject, dummy.EchoConfig, nil)).To(BeFalse())
		})

		It("doesn't grant the whole object type", func() {
			Expect(rbac.Check(DefaultContext, carol.ID.String(), policy.ObjectPlaybooks, policy.ActionPlaybookRun)).To(BeFalse())
		})

		It("keeps each rule's playbooks paired with its targets", func() {
			subject := bob.ID.String()
			Expect(canRunOn(subject, dummy.RestartPod, &tenantA)).To(BeTrue())
			Expect(canRunOn(subject, dummy.EchoConfig, &tenantB)).To(BeTrue())
			Expect(canRunOn(subject, dummy.EchoConfig, &tenantA)).To(BeFalse(), "echo-config is only granted on tenant b")
		})
	})

	Describe("deny rules", func() {
		It("make a stored role Invalid, granting nothing", func() {
			role := newRole("no-tenant-b", namespace,
				on(allow("run", policy.ActionPlaybookRun, "all-playbooks"), "all-configs"),
				deny(on(allow("no-runs", policy.ActionPlaybookRun, "all-playbooks"), "tenant-b")))
			err := db.PersistRoleFromCRD(DefaultContext, role)
			var notReady *kopper.NotReadyError
			Expect(errors.As(err, &notReady)).To(BeTrue(), "%v", err)
			Expect(notReady.Reason).To(Equal(kopper.ReasonInvalid))
			Expect(notReady.Message).To(ContainSubstring("deny isn't supported"))

			binding := newBinding("no-tenant-b", role.Name, people(erin))
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).ToNot(Succeed())
			DeferCleanup(func() {
				Expect(DefaultContext.DB().Where("id = ?", binding.UID).Delete(&models.RoleBinding{}).Error).To(Succeed())
				Expect(DefaultContext.DB().Where("id = ?", role.UID).Delete(&models.Role{}).Error).To(Succeed())
				Expect(rbac.ReloadPolicy()).To(Succeed())
			})
			Expect(rbac.ReloadPolicy()).To(Succeed())

			Expect(*storedRole(role.UID).ErrorReason).To(Equal(adapter.ReasonInvalid))
			Expect(*storedBinding(namespace, binding.Name).ErrorReason).To(Equal(adapter.ReasonRoleInvalid))
			Expect(canRunOn(erin.ID.String(), dummy.EchoConfig, &tenantA)).To(BeFalse(), "the role's allow rule doesn't apply either")
			Expect(canRunOn(erin.ID.String(), dummy.EchoConfig, &tenantB)).To(BeFalse())
		})

		It("apply a role whose rules set deny: false", func() {
			rule := on(allow("run", policy.ActionPlaybookRun, "all-playbooks"), "tenant-b")
			rule.Deny = false
			role := newRole("deny-false", namespace, rule)
			persistRole(role)

			binding := newBinding("deny-false", role.Name, people(erin))
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).To(Succeed())
			DeferCleanup(func() {
				Expect(DefaultContext.DB().Where("id = ?", binding.UID).Delete(&models.RoleBinding{}).Error).To(Succeed())
				Expect(DefaultContext.DB().Where("id = ?", role.UID).Delete(&models.Role{}).Error).To(Succeed())
				Expect(rbac.ReloadPolicy()).To(Succeed())
			})
			Expect(rbac.ReloadPolicy()).To(Succeed())

			Expect(canRunOn(erin.ID.String(), dummy.EchoConfig, &tenantB)).To(BeTrue())
			Expect(canRunOn(erin.ID.String(), dummy.EchoConfig, &tenantA)).To(BeFalse())
		})

		It("never load as casbin deny policies", func() {
			Expect(rbac.ReloadPolicy()).To(Succeed())
			policies, err := rbac.Enforcer().GetPolicy()
			Expect(err).ToNot(HaveOccurred())
			Expect(policies).ToNot(BeEmpty())
			for _, p := range policies {
				Expect(p[3]).To(Equal("allow"), "%v", p)
			}
		})
	})

	Describe("constraints", func() {
		It("narrow the target of a rule", func() {
			subject := dave.ID.String()
			Expect(canRunOn(subject, dummy.RestartPod, &tenantA)).To(BeTrue())
			Expect(canRunOn(subject, dummy.RestartPod, &tenantB)).To(BeFalse())
			Expect(canRunOn(subject, dummy.EchoConfig, &tenantA)).To(BeFalse(), "the role's resource still applies")
		})

		It("narrow the resource of a rule", func() {
			subject := dave.ID.String()
			Expect(canRead(subject, tenantA)).To(BeTrue())
			Expect(canRead(subject, tenantB)).To(BeFalse())
		})

		It("leave the role as defined for bindings without constraints", func() {
			subject := rlsGuest.ID.String()
			Expect(canRead(subject, tenantA)).To(BeTrue())
			Expect(canRead(subject, tenantB)).To(BeTrue())
		})

		It("grant the whole role, and don't apply an allow rule they can't narrow", func() {
			role := newRole("whole-role", namespace,
				allow("read", policy.ActionRead, "role-configs"),
				on(allow("run", policy.ActionPlaybookRun, "all-playbooks"), "all-configs"))
			persistRole(role)

			binding := constrained(newBinding("whole-role", role.Name, people(erin)), "", "tenant-a")
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).To(Succeed(), "ready while an allow rule applies")
			Expect(rbac.ReloadPolicy()).To(Succeed())

			subject := erin.ID.String()
			Expect(canRunOn(subject, dummy.EchoConfig, &tenantA)).To(BeTrue(), "narrowed to the constraint's target")
			Expect(canRunOn(subject, dummy.EchoConfig, &tenantB)).To(BeFalse())
			Expect(canRead(subject, tenantA)).To(BeFalse(), "read takes no target, so a target constraint can't narrow it")

			condition := meta.FindStatusCondition(binding.Status.Conditions, v1.ConditionAllRulesApply)
			Expect(condition).ToNot(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(adapter.ReasonConstraintDoesNotFit))
			Expect(condition.Message).To(ContainSubstring("rule read:"))

			Expect(db.PersistRoleFromCRD(DefaultContext, role)).To(Succeed())
			Expect(role.Status.BindingsWithUnappliedRules).To(ConsistOf(v1.BindingUnappliedRules{Name: "whole-role", Rules: []string{"read"}}))
		})

		It("report every allow rule as applying when the constraint narrows them all", func() {
			binding := constrained(newBinding("tenant-a-operators", "read-configs", people(dave, rlsTenantGuest)), "tenant-a-boundary", "tenant-a-boundary")
			binding.UID = k8sTypes.UID(storedBinding(namespace, "tenant-a-operators").ID.String())
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).To(Succeed())
			Expect(meta.IsStatusConditionTrue(binding.Status.Conditions, v1.ConditionAllRulesApply)).To(BeTrue())
		})

		It("narrow a rule without a target to nothing under a target constraint", func() {
			binding := constrained(newBinding("targetless", "run-playbooks", people(erin)), "all-playbooks", "tenant-a")
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).ToNot(Succeed())
			DeferCleanup(func() {
				Expect(DefaultContext.DB().Where("id = ?", binding.UID).Delete(&models.RoleBinding{}).Error).To(Succeed())
				Expect(rbac.ReloadPolicy()).To(Succeed())
			})
			Expect(rbac.ReloadPolicy()).To(Succeed())

			Expect(*storedBinding(namespace, binding.Name).ErrorReason).To(Equal(adapter.ReasonNoRulesApply))
			Expect(meta.IsStatusConditionFalse(binding.Status.Conditions, v1.ConditionAllRulesApply)).To(BeTrue())
			Expect(canRunOn(erin.ID.String(), dummy.EchoConfig, nil)).To(BeFalse())
		})

		It("ignore types of the constraint's scope an input can't carry", func() {
			Expect(canRunOn(dave.ID.String(), dummy.RestartPod, &tenantA)).To(BeTrue(), "tenant-a-boundary's playbooks are ignored on the target side")
		})
	})

	Describe("whole object types", func() {
		It("grants update on every config as the catalog object", func() {
			role := newRole("update-configs", namespace, allow("update", policy.ActionUpdate, "all-configs"))
			persistRole(role)
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, newBinding("update-configs", role.Name, people(erin)))).To(Succeed())
			Expect(rbac.ReloadPolicy()).To(Succeed())

			Expect(rbac.Check(DefaultContext, erin.ID.String(), policy.ObjectCatalog, policy.ActionUpdate)).To(BeTrue())
			Expect(rbac.Check(DefaultContext, erin.ID.String(), policy.ObjectTopology, policy.ActionUpdate)).To(BeFalse())
		})
	})

	Describe("validation", func() {
		// rejectRole stores the role as written, and expects it to be invalid: Ready=False, with the reason recorded.
		rejectRole := func(rules ...v1.RoleRule) {
			GinkgoHelper()
			role := newRole("rejected", namespace, rules...)
			Expect(db.PersistRoleFromCRD(DefaultContext, role)).ToNot(Succeed())
			stored := storedRole(role.UID)
			Expect(stored.Error).ToNot(BeNil())
			Expect(stored.ErrorReason).ToNot(BeNil())
			Expect(DefaultContext.DB().Delete(&stored).Error).To(Succeed())
		}

		It("rejects actions without a contract", func() {
			rejectRole(allow("unknown", "playbook:read", "all-playbooks"))
			rejectRole(allow("pattern", "playbook:*", "all-playbooks"))
			rejectRole(allow("all", "*", "all-playbooks"))
			rejectRole(allow("plugin-role", "plugin-role:kubernetes:admin", "all-configs"))
		})

		It("rejects a resource on an action that takes none", func() {
			rejectRole(allow("mcp-on-playbooks", policy.ActionMCPUse, "all-playbooks"))
			rejectRole(allow("people-on-playbooks", adapter.ActionPersonManage, "all-playbooks"))
		})

		It("rejects scopes the action doesn't accept", func() {
			rejectRole(allow("views", policy.ActionPlaybookRun, "staging-views"))
			rejectRole(allow("mixed", policy.ActionPlaybookRun, "configs-and-playbooks"))
			rejectRole(on(allow("target-playbooks", policy.ActionPlaybookRun, "all-playbooks"), "all-playbooks"))
			rejectRole(allow("read-views", policy.ActionRead, "staging-views"))
		})

		It("accepts reading checks by name", func() {
			persistRole(newRole("read-http-checks", namespace, allow("read", policy.ActionRead, "http-checks")))
		})

		It("accepts a target scope with several accepted types", func() {
			Expect(db.PersistScopeFromCRD(DefaultContext, newScope("staging-targets", namespace,
				v1.ScopeTarget{Config: &v1.ScopeConfigSelector{TagSelector: "namespace=staging"}},
				v1.ScopeTarget{Component: &v1.ScopeLabelledSelector{ScopeResourceRef: v1.ScopeResourceRef{Namespace: "staging"}}},
				v1.ScopeTarget{Check: &v1.ScopeLabelledSelector{ScopeResourceRef: v1.ScopeResourceRef{Namespace: "staging"}}}))).To(Succeed())
			persistRole(newRole("run-on-staging", namespace, on(allow("run", policy.ActionPlaybookRun, "all-playbooks"), "staging-targets")))
		})

		It("rejects targets on actions without one", func() {
			rejectRole(on(allow("read", policy.ActionRead, "role-configs"), "all-configs"))
		})

		It("accepts read rules over any scope: membership is decided by the resource alone", func() {
			role := newRole("components-by-namespace", namespace, allow("components-by-namespace", policy.ActionRead, "staging-components"))
			Expect(db.PersistRoleFromCRD(DefaultContext, role)).To(Succeed())
			stored := storedRole(role.UID)
			Expect(stored.Error).To(BeNil())
			Expect(DefaultContext.DB().Delete(&stored).Error).To(Succeed())
		})

		It("rejects read rules over connections that aren't whole types", func() {
			scope := newScope("aws-connections", namespace, v1.ScopeTarget{Connection: &v1.ScopeConnectionSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "aws"}}})
			Expect(db.PersistScopeFromCRD(DefaultContext, scope)).To(Succeed())
			DeferCleanup(func() {
				Expect(DefaultContext.DB().Delete(&models.Scope{}, "id = ?", scope.UID).Error).To(Succeed())
			})

			rejectRole(allow("aws-connections", policy.ActionRead, "aws-connections"))
		})

		It("rejects deny rules", func() {
			rejectRole(deny(allow("no-read", policy.ActionRead, "role-configs")))
			rejectRole(deny(on(allow("no-runs", policy.ActionPlaybookRun, "all-playbooks"), "tenant-b")))
		})

		It("rejects create, update and delete on anything but whole types", func() {
			rejectRole(allow("update-tenant", policy.ActionUpdate, "tenant-a"))
		})

		It("rejects missing scopes and duplicate rule names", func() {
			rejectRole(allow("missing", policy.ActionPlaybookRun, "does-not-exist"))
			rejectRole(allow("same", policy.ActionPlaybookRun, "all-playbooks"), allow("same", policy.ActionRead, "role-configs"))
		})

		It("rejects roles named after a built-in role", func() {
			role := newRole(policy.RoleViewer, namespace, allow("run", policy.ActionPlaybookRun, "all-playbooks"))
			Expect(db.PersistRoleFromCRD(DefaultContext, role)).ToNot(Succeed())
			stored := storedRole(role.UID)
			Expect(stored.Error).ToNot(BeNil())
			Expect(DefaultContext.DB().Delete(&stored).Error).To(Succeed())
		})

		It("stores a role with one invalid rule, and applies none of its rules", func() {
			role := newRole("partly-invalid", namespace,
				allow("valid", policy.ActionPlaybookRun, "all-playbooks"),
				allow("invalid", policy.ActionPlaybookRun, "staging-views"))
			Expect(db.PersistRoleFromCRD(DefaultContext, role)).ToNot(Succeed())

			binding := newBinding("partly-invalid", role.Name, people(erin))
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).ToNot(Succeed())
			DeferCleanup(func() {
				Expect(DefaultContext.DB().Where("id = ?", binding.UID).Delete(&models.RoleBinding{}).Error).To(Succeed())
				Expect(DefaultContext.DB().Where("id = ?", role.UID).Delete(&models.Role{}).Error).To(Succeed())
				Expect(rbac.ReloadPolicy()).To(Succeed())
			})
			Expect(rbac.ReloadPolicy()).To(Succeed())

			Expect(canRunOn(erin.ID.String(), dummy.EchoConfig, nil)).To(BeFalse())
			Expect(*storedBinding(namespace, binding.Name).ErrorReason).To(Equal(adapter.ReasonRoleInvalid))
		})

		// rejectBinding stores the binding as written, and expects it to be invalid.
		rejectBinding := func(role string) {
			GinkgoHelper()
			binding := newBinding("rejected", role, people(erin))
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).ToNot(Succeed())
			stored := storedBinding(namespace, binding.Name)
			Expect(stored.Error).ToNot(BeNil())
			Expect(DefaultContext.DB().Delete(&stored).Error).To(Succeed())
		}

		It("rejects a constraint that sets neither resource nor target", func() {
			spec := v1.RoleBindingSpec{Role: "read-configs", Subjects: people(erin), Constraint: &v1.RoleBindingConstraint{}}
			Expect(spec.Validate()).To(MatchError(ContainSubstring("constraint must set resource or target")))

			binding := newBinding("empty-constraint", "read-configs", people(erin))
			binding.Spec.Constraint = &v1.RoleBindingConstraint{}
			err := db.PersistRoleBindingFromCRD(DefaultContext, binding)
			Expect(err).To(MatchError(ContainSubstring("constraint must set resource or target")))
			Expect(err).ToNot(BeAssignableToTypeOf(&kopper.NotReadyError{}), "never stored, so never granted without its constraint")

			var count int64
			Expect(DefaultContext.DB().Model(&models.RoleBinding{}).Where("id = ?", binding.UID).Count(&count).Error).To(Succeed())
			Expect(count).To(BeZero())
		})

		It("rejects a binding until its role exists", func() {
			rejectBinding("does-not-exist")
		})

		It("rejects people by name, and resource subjects with patterns", func() {
			for _, subjects := range []v1.RoleBindingSubjects{
				{PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{"Role Erin"}}},
				{PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{"*"}}},
				{PermissionGroupSubjects: v1.PermissionGroupSubjects{Playbooks: []v1.PermissionGroupSelector{{}}}},
				{PermissionGroupSubjects: v1.PermissionGroupSubjects{Playbooks: []v1.PermissionGroupSelector{{Namespace: "*"}}}},
				{PermissionGroupSubjects: v1.PermissionGroupSubjects{Scrapers: []v1.PermissionGroupSelector{{Name: "prod-*"}}}},
			} {
				binding := newBinding("rejected-subjects", "run-playbooks", subjects)
				Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).ToNot(Succeed(), "%+v", subjects)
				stored := storedBinding(namespace, binding.Name)
				Expect(stored.Error).ToNot(BeNil())
				Expect(DefaultContext.DB().Delete(&stored).Error).To(Succeed())
			}
		})
	})

	Describe("plugin subjects", Ordered, func() {
		const pluginNamespace = "role-plugins"

		var (
			human       *models.Person
			logsPlugin  models.Plugin
			otherPlugin models.Plugin
		)

		canReadConnections := func(subject string) bool {
			return rbac.Check(DefaultContext, subject, policy.ObjectConnection, policy.ActionRead)
		}

		createPlugin := func(name string) models.Plugin {
			GinkgoHelper()
			p := models.Plugin{ID: uuid.New(), Name: name, Namespace: pluginNamespace, Source: models.SourceCRD, Spec: []byte(`{}`)}
			Expect(DefaultContext.DB().Create(&p).Error).To(Succeed())
			return p
		}

		// bind persists a binding, and reloads the policy so it's in effect.
		bind := func(binding *v1.RoleBinding) error {
			GinkgoHelper()
			err := db.PersistRoleBindingFromCRD(DefaultContext, binding)
			DeferCleanup(func() {
				Expect(DefaultContext.DB().Where("id = ?", binding.UID).Delete(&models.RoleBinding{}).Error).To(Succeed())
				Expect(rbac.ReloadPolicy()).To(Succeed())
			})
			Expect(rbac.ReloadPolicy()).To(Succeed())
			return err
		}

		BeforeAll(func() {
			human = setup.CreateUserWithRole(DefaultContext, "Role Human", "role-human@test.com")
			logsPlugin = createPlugin("kubernetes-logs")
			otherPlugin = createPlugin("other")

			for _, scope := range []*v1.Scope{
				newScope("all-connections", namespace, v1.ScopeTarget{Connection: &v1.ScopeConnectionSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}}),
				newScope("connections-and-configs", namespace,
					v1.ScopeTarget{Connection: &v1.ScopeConnectionSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}},
					v1.ScopeTarget{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}}),
			} {
				Expect(db.PersistScopeFromCRD(DefaultContext, scope)).To(Succeed())
			}

			for _, role := range []*v1.Role{
				newRole("read-connections", namespace, allow("read", policy.ActionRead, "all-connections")),
				newRole("read-connections-and-run", namespace,
					allow("read", policy.ActionRead, "all-connections"),
					allow("run", policy.ActionPlaybookRun, "all-playbooks")),
				newRole("read-connections-and-configs", namespace, allow("read", policy.ActionRead, "connections-and-configs")),
			} {
				persistRole(role)
			}
		})

		AfterAll(func() {
			Expect(DefaultContext.DB().Where("namespace = ?", pluginNamespace).Delete(&models.Plugin{}).Error).To(Succeed())
			Expect(DefaultContext.DB().Delete(human).Error).To(Succeed())
			Expect(rbac.ReloadPolicy()).To(Succeed())
		})

		It("grants the role to plugins by namespace and name", func() {
			Expect(bind(newBinding("logs-plugin", "read-connections", v1.RoleBindingSubjects{
				Plugins: []v1.PermissionGroupSelector{{Namespace: pluginNamespace, Name: logsPlugin.Name}},
			}))).To(Succeed())

			Expect(canReadConnections(v1.PluginSubject(pluginNamespace, logsPlugin.Name))).To(BeTrue())
			Expect(canReadConnections(v1.PluginSubject(pluginNamespace, otherPlugin.Name))).To(BeFalse())
		})

		It("grants the role to every plugin of a namespace", func() {
			Expect(bind(newBinding("namespace-plugins", "read-connections", v1.RoleBindingSubjects{
				Plugins: []v1.PermissionGroupSelector{{Namespace: pluginNamespace}},
			}))).To(Succeed())

			Expect(canReadConnections(v1.PluginSubject(pluginNamespace, logsPlugin.Name))).To(BeTrue())
			Expect(canReadConnections(v1.PluginSubject(pluginNamespace, otherPlugin.Name))).To(BeTrue())
			Expect(canReadConnections(v1.PluginSubject("elsewhere", logsPlugin.Name))).To(BeFalse())
		})

		for _, tt := range []struct{ name, role string }{
			{"a rule with another action", "read-connections-and-run"},
			{"a rule whose scope selects another type", "read-connections-and-configs"},
		} {
			It("grants nothing, to any subject, when a plugin is bound to a role with "+tt.name, func() {
				binding := newBinding("restricted-"+tt.role, tt.role, v1.RoleBindingSubjects{
					PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{human.Email}},
					Plugins:                 []v1.PermissionGroupSelector{{Namespace: pluginNamespace, Name: logsPlugin.Name}},
				})
				Expect(bind(binding)).To(BeAssignableToTypeOf(&kopper.NotReadyError{}))

				stored := storedBinding(namespace, binding.Name)
				Expect(stored.ErrorReason).To(HaveValue(Equal(adapter.ReasonPluginRuleNotAllowed)))

				for _, subject := range []string{human.ID.String(), v1.PluginSubject(pluginNamespace, logsPlugin.Name)} {
					Expect(canReadConnections(subject)).To(BeFalse(), subject)
					hasRole, err := rbac.Enforcer().HasRoleForUser(subject, models.BindingPrincipal(namespace, binding.Name))
					Expect(err).ToNot(HaveOccurred())
					Expect(hasRole).To(BeFalse(), subject)
				}
			})
		}

		It("applies the same role when no plugin is selected", func() {
			Expect(bind(newBinding("run-without-plugins", "read-connections-and-run", people(human)))).To(Succeed())

			Expect(canReadConnections(human.ID.String())).To(BeTrue())
		})
	})

	Describe("lifecycle", func() {
		It("narrows a rule added to a role, or reports it when the constraint can't narrow it", func() {
			role := roles[namespace+"/read-configs"]
			changed := map[string]bool{}
			adapter.ValidityChanged = func(table, ns, name, source string) { changed[table+"/"+ns+"/"+name] = true }
			DeferCleanup(func() { adapter.ValidityChanged = nil })

			added := newRole(role.Name, namespace, append(role.Spec.Rules, allow("update", policy.ActionUpdate, "all-configs"))...)
			added.UID = role.UID
			Expect(db.PersistRoleFromCRD(DefaultContext, added)).To(Succeed(), "a role is never validated against its bindings")
			Expect(added.Status.BindingsWithUnappliedRules).To(ContainElement(v1.BindingUnappliedRules{Name: "tenant-a-operators", Rules: []string{"update"}}))
			Expect(rbac.ReloadPolicy()).To(Succeed())

			Expect(changed).To(HaveKey("role_bindings/"+namespace+"/tenant-a-operators"), "the binding's CRD is reconciled again, so its AllRulesApply condition follows")
			Expect(changed).To(HaveKey("roles/" + namespace + "/read-configs"))
			Expect(storedBinding(namespace, "tenant-a-operators").Error).To(BeNil(), "the binding stays ready")
			Expect(canRunOn(dave.ID.String(), dummy.RestartPod, &tenantA)).To(BeTrue(), "the other rules keep applying")
			Expect(canRead(dave.ID.String(), tenantA)).To(BeTrue())
			Expect(rbac.Check(DefaultContext, dave.ID.String(), policy.ObjectCatalog, policy.ActionUpdate)).To(BeFalse(), "never granted as written")

			compiled, err := adapter.ValidateBinding(DefaultContext, nil, storedBinding(namespace, "tenant-a-operators"))
			Expect(err).ToNot(HaveOccurred())
			Expect(compiled.Unapplied).To(HaveLen(1))
			Expect(compiled.Unapplied[0].Rule).To(Equal("update"))

			persistRole(role)
			Expect(role.Status.BindingsWithUnappliedRules).ToNot(ContainElement(HaveField("Name", "tenant-a-operators")))
			Expect(rbac.ReloadPolicy()).To(Succeed())
		})

		It("lets a scope change that breaks a rule through, and the role stops applying until it's fixed", func() {
			var stored models.Scope
			Expect(DefaultContext.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", namespace, "kubernetes-playbooks").First(&stored).Error).To(Succeed())
			Expect(canRunOn(bob.ID.String(), dummy.RestartPod, &tenantA)).To(BeTrue())

			changed := newScope("kubernetes-playbooks", namespace, v1.ScopeTarget{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}})
			changed.UID = k8sTypes.UID(stored.ID.String())
			Expect(db.PersistScopeFromCRD(DefaultContext, changed)).To(Succeed(), "a scope is never validated against what references it")
			Expect(rbac.ReloadPolicy()).To(Succeed())

			Expect(*storedRole(roles[namespace+"/tenant-a-runner"].UID).ErrorReason).To(Equal(adapter.ReasonInvalid))
			Expect(canRunOn(bob.ID.String(), dummy.RestartPod, &tenantA)).To(BeFalse())

			restored := newScope("kubernetes-playbooks", namespace, v1.ScopeTarget{Playbook: &v1.ScopePlaybookRef{Name: dummy.RestartPod.Name}})
			restored.UID = changed.UID
			Expect(db.PersistScopeFromCRD(DefaultContext, restored)).To(Succeed())
			Expect(rbac.ReloadPolicy()).To(Succeed())

			Expect(storedRole(roles[namespace+"/tenant-a-runner"].UID).Error).To(BeNil())
			Expect(canRunOn(bob.ID.String(), dummy.RestartPod, &tenantA)).To(BeTrue())
		})

		It("deletes a scope rules reference, and they stop granting until it's back", func() {
			var stored models.Scope
			Expect(DefaultContext.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", namespace, "tenant-a").First(&stored).Error).To(Succeed())
			changed := map[string]bool{}
			adapter.ValidityChanged = func(table, ns, name, source string) { changed[table+"/"+ns+"/"+name] = true }
			DeferCleanup(func() { adapter.ValidityChanged = nil })

			Expect(db.DeleteScope(DefaultContext, stored.ID.String())).To(Succeed())
			Expect(rbac.ReloadPolicy()).To(Succeed())

			Expect(changed).To(HaveKey("roles/"+namespace+"/tenant-a-runner"), "the role's CRD is reconciled again, so its Ready condition follows")
			Expect(*storedRole(roles[namespace+"/tenant-a-runner"].UID).ErrorReason).To(Equal(adapter.ReasonScopeNotFound))
			Expect(canRunOn(bob.ID.String(), dummy.RestartPod, &tenantA)).To(BeFalse())

			Expect(db.PersistScopeFromCRD(DefaultContext, newScope("tenant-a", namespace,
				v1.ScopeTarget{Config: &v1.ScopeConfigSelector{TagSelector: "tenant=a"}}))).To(Succeed())
			Expect(rbac.ReloadPolicy()).To(Succeed())
			Expect(canRunOn(bob.ID.String(), dummy.RestartPod, &tenantA)).To(BeTrue())
		})

		It("deletes a scope a constraint references, and the binding applies no allow rule until it's back", func() {
			var stored models.Scope
			Expect(DefaultContext.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", namespace, "tenant-a-boundary").First(&stored).Error).To(Succeed())
			changed := map[string]bool{}
			adapter.ValidityChanged = func(table, ns, name, source string) { changed[table+"/"+ns+"/"+name] = true }
			DeferCleanup(func() { adapter.ValidityChanged = nil })

			Expect(db.DeleteScope(DefaultContext, stored.ID.String())).To(Succeed())
			Expect(rbac.ReloadPolicy()).To(Succeed())

			Expect(changed).To(HaveKey("role_bindings/" + namespace + "/tenant-a-operators"))
			Expect(*storedBinding(namespace, "tenant-a-operators").ErrorReason).To(Equal(adapter.ReasonScopeNotFound))
			Expect(canRunOn(dave.ID.String(), dummy.RestartPod, &tenantA)).To(BeFalse())
			Expect(canRead(dave.ID.String(), tenantA)).To(BeFalse(), "never granted as written")

			Expect(db.PersistScopeFromCRD(DefaultContext, newScope("tenant-a-boundary", namespace,
				v1.ScopeTarget{Config: &v1.ScopeConfigSelector{TagSelector: "tenant=a"}},
				v1.ScopeTarget{Playbook: &v1.ScopePlaybookRef{Name: "*"}}))).To(Succeed())
			Expect(rbac.ReloadPolicy()).To(Succeed())
			Expect(storedBinding(namespace, "tenant-a-operators").Error).To(BeNil())
			Expect(canRunOn(dave.ID.String(), dummy.RestartPod, &tenantA)).To(BeTrue())
		})

		It("applies none of an invalid role's rules", func() {
			broken := models.Role{
				ID:        uuid.New(),
				Name:      "broken",
				Namespace: namespace,
				Source:    models.SourceCRD,
				Rules:     []byte(`[{"name":"no-runs","action":"playbook:run","resource":{"scopeRef":"does-not-exist"}}]`),
			}
			Expect(DefaultContext.DB().Create(&broken).Error).To(Succeed())

			subjects, err := json.Marshal(people(alice))
			Expect(err).ToNot(HaveOccurred())
			binding := models.RoleBinding{
				ID: uuid.New(), Name: "broken", Namespace: namespace, Source: models.SourceCRD,
				Role: broken.Name, Subjects: subjects,
			}
			Expect(DefaultContext.DB().Create(&binding).Error).To(Succeed())
			DeferCleanup(func() {
				Expect(DefaultContext.DB().Delete(&binding).Error).To(Succeed())
				Expect(DefaultContext.DB().Delete(&broken).Error).To(Succeed())
				Expect(rbac.ReloadPolicy()).To(Succeed())
			})
			Expect(rbac.ReloadPolicy()).To(Succeed())

			Expect(*storedRole(k8sTypes.UID(broken.ID.String())).ErrorReason).To(Equal(adapter.ReasonScopeNotFound))
			Expect(*storedBinding(namespace, "broken").ErrorReason).To(Equal(adapter.ReasonRoleInvalid))
		})

		It("revokes access when the binding is deleted", func() {
			var binding models.RoleBinding
			Expect(DefaultContext.DB().Where("namespace = ? AND name = ?", namespace, "people-and-teams").First(&binding).Error).To(Succeed())
			Expect(db.DeleteRoleBinding(DefaultContext, binding.ID.String())).To(Succeed())
			Expect(rbac.ReloadPolicy()).To(Succeed())

			Expect(canRunOn(alice.ID.String(), dummy.EchoConfig, nil)).To(BeFalse())
		})

		It("revokes the role's rules when the role is deleted", func() {
			Expect(db.DeleteRole(DefaultContext, string(roles[namespace+"/run-playbooks"].UID))).To(Succeed())
			Expect(rbac.ReloadPolicy()).To(Succeed())

			Expect(canRunOn(builtinAgent.ID.String(), dummy.EchoConfig, nil)).To(BeFalse())
		})
	})

	Describe("row filters", func() {
		// grant lists the rows in the named Scope, narrowed by the constraint's Scope when one is named.
		grant := func(scopes ...string) *rls.Grants {
			GinkgoHelper()
			g := rls.NoRows()
			ids := lo.Map(scopes, func(name string, _ int) string {
				var scope models.Scope
				Expect(DefaultContext.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", namespace, name).First(&scope).Error).To(Succeed())
				return scope.ID.String()
			})
			grant := rls.Grant{Scope: ids[0]}
			if len(ids) > 1 {
				grant.Constraint = ids[1]
			}
			g.Add(grant)
			return g
		}

		It("filters rows by the role's read rules", func() {
			payload, err := auth.GetRLSPayload(DefaultContext.WithUser(rlsGuest))
			Expect(err).ToNot(HaveOccurred())
			Expect(payload.Disable).To(BeFalse())
			Expect(payload.Config).To(Equal(grant("role-configs")))
		})

		It("filters rows by the rule's scope and the constraint's", func() {
			payload, err := auth.GetRLSPayload(DefaultContext.WithUser(rlsTenantGuest))
			Expect(err).ToNot(HaveOccurred())
			Expect(payload.Config).To(Equal(grant("role-configs", "tenant-a-boundary")))
		})

		It("grants no rows of generated view tables", func() {
			payload, err := auth.GetRLSPayload(DefaultContext.WithUser(rlsGuest))
			Expect(err).ToNot(HaveOccurred())
			Expect(payload.Scopes).To(BeEmpty())
		})

		It("filters rows for federated users by their bindings only", func() {
			federatedUser := func(username string, bindings ...string) (*models.Person, string) {
				person, err := db.GetOrCreateFederatedPerson(DefaultContext, "appx", username, username, "")
				Expect(err).ToNot(HaveOccurred())

				subject := models.FederatedPrincipal(person.ID.String())
				if len(bindings) > 0 {
					Expect(rbac.AddRoleForUser(subject, bindings...)).To(Succeed())
				}

				DeferCleanup(func() {
					Expect(rbac.DeleteAllRolesForUser(subject)).To(Succeed())
					Expect(DefaultContext.DB().Delete(person).Error).To(Succeed())
				})
				return person, subject
			}

			reader, readerSubject := federatedUser("rls-reader", models.BindingPrincipal(namespace, "tenant-a-operators"))
			payload, err := auth.GetRLSPayload(DefaultContext.WithUser(reader).WithSubject(readerSubject))
			Expect(err).ToNot(HaveOccurred())
			Expect(payload.Disable).To(BeFalse())
			Expect(payload.Config).To(Equal(grant("role-configs", "tenant-a-boundary")))

			listStatus := func(person *models.Person, subject, table string) int {
				GinkgoHelper()
				e := echo.New()
				req := httptest.NewRequest(http.MethodGet, "/db/"+table, nil)
				req = req.WithContext(DefaultContext.WithUser(person).WithSubject(subject))
				rec := httptest.NewRecorder()
				handler := mcRBAC.DbMiddleware()(func(c echo.Context) error { return c.NoContent(http.StatusOK) })
				Expect(handler(e.NewContext(req, rec))).To(Succeed())
				return rec.Code
			}
			visibleNames := func(person *models.Person, subject string) []uuid.UUID {
				GinkgoHelper()
				var ids []uuid.UUID
				Expect(auth.WithRLS(DefaultContext.WithUser(person).WithSubject(subject), func(ctx context.Context) error {
					return ctx.DB().Table("config_names").Where("id IN ?", []uuid.UUID{tenantA.ID, tenantB.ID}).Pluck("id", &ids).Error
				})).To(Succeed())
				return ids
			}
			configViews := []string{
				"catalog_changes", "config_class_summary", "config_classes", "config_detail", "config_labels", "config_names",
				"config_statuses", "config_summary", "config_tags", "config_tags_labels_keys", "config_types",
			}

			Expect(listStatus(reader, readerSubject, "config_items")).To(Equal(http.StatusOK), "its read grants cover some configs, filtered by row")
			Expect(listStatus(reader, readerSubject, "components")).To(Equal(http.StatusForbidden), "no read grant covers components")
			for _, view := range configViews {
				Expect(listStatus(reader, readerSubject, view)).To(Equal(http.StatusOK), "views of configs are filtered by row: %s", view)
			}
			Expect(visibleNames(reader, readerSubject)).To(Equal([]uuid.UUID{tenantA.ID}))

			admin := newAdmin("Role RLS Admin", "role-rls-admin@test.com")
			DeferCleanup(func() { Expect(DefaultContext.DB().Delete(admin).Error).To(Succeed()) })
			for _, view := range configViews {
				Expect(listStatus(admin, admin.ID.String(), view)).To(Equal(http.StatusOK), "a whole-type reader reads %s", view)
			}
			Expect(visibleNames(admin, admin.ID.String())).To(ConsistOf([]uuid.UUID{tenantA.ID, tenantB.ID}))

			nobody, nobodySubject := federatedUser("rls-nobody")
			Expect(listStatus(nobody, nobodySubject, "config_items")).To(Equal(http.StatusForbidden))
			for _, view := range configViews {
				Expect(listStatus(nobody, nobodySubject, view)).To(Equal(http.StatusForbidden), "no read grant covers %s", view)
			}
			payload, err = auth.GetRLSPayload(DefaultContext.WithUser(nobody).WithSubject(nobodySubject))
			Expect(err).ToNot(HaveOccurred())
			Expect(payload.Disable).To(BeFalse())
			Expect(payload).To(Equal(&rls.Payload{}))
		})

		It("summarizes the catalog for a guest whose only read of configs is a Scope", func() {
			e := echo.New()
			catalog.RegisterRoutes(e)
			summary := func(person *models.Person) (int, []string) {
				GinkgoHelper()
				req := httptest.NewRequest(http.MethodPost, "/catalog/summary", strings.NewReader(`{}`))
				req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
				req = req.WithContext(DefaultContext.WithUser(person))
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					return rec.Code, nil
				}
				var rows []map[string]any
				Expect(json.Unmarshal(rec.Body.Bytes(), &rows)).To(Succeed())
				return rec.Code, lo.Map(rows, func(row map[string]any, _ int) string { return row["type"].(string) })
			}

			status, types := summary(rlsGuest)
			Expect(status).To(Equal(http.StatusOK))
			Expect(types).To(ConsistOf("Role::Test"), "only the configs its Scope selects are summarized")

			admin := newAdmin("Role Summary Admin", "role-summary-admin@test.com")
			DeferCleanup(func() { Expect(DefaultContext.DB().Delete(admin).Error).To(Succeed()) })
			status, types = summary(admin)
			Expect(status).To(Equal(http.StatusOK))
			Expect(types).To(ContainElement("Role::Test"))
			Expect(len(types)).To(BeNumerically(">", 1), "a whole-type reader summarizes every config")

			stranger := setup.CreateUserWithRole(DefaultContext, "Role Summary Stranger", "role-summary-stranger@test.com")
			DeferCleanup(func() { Expect(DefaultContext.DB().Delete(stranger).Error).To(Succeed()) })
			Expect(summary(stranger)).To(Equal(http.StatusForbidden), "a guest without read grants on configs")
		})

		It("lists checks only through a read of checks, not of canaries", func() {
			for _, scope := range []*v1.Scope{
				newScope("every-canary", namespace, v1.ScopeTarget{Canary: &v1.ScopeCanarySelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}}),
				newScope("every-check", namespace, v1.ScopeTarget{Check: &v1.ScopeLabelledSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}}),
			} {
				Expect(db.PersistScopeFromCRD(DefaultContext, scope)).To(Succeed())
			}
			persistRole(newRole("read-every-canary", namespace, allow("read", policy.ActionRead, "every-canary")))
			persistRole(newRole("read-every-check", namespace, allow("read", policy.ActionRead, "every-check")))
			for _, b := range []*v1.RoleBinding{
				newBinding("read-every-canary", "read-every-canary", people(alice)),
				newBinding("read-every-check", "read-every-check", people(alice)),
			} {
				Expect(db.PersistRoleBindingFromCRD(DefaultContext, b)).To(Succeed())
			}
			Expect(rbac.ReloadPolicy()).To(Succeed())

			listStatus := func(bindingName, table string) int {
				GinkgoHelper()
				person, err := db.GetOrCreateFederatedPerson(DefaultContext, "appx", "lister-"+bindingName, "lister", "")
				Expect(err).ToNot(HaveOccurred())
				subject := models.FederatedPrincipal(person.ID.String())
				Expect(rbac.AddRoleForUser(subject, models.BindingPrincipal(namespace, bindingName))).To(Succeed())
				DeferCleanup(func() {
					Expect(rbac.DeleteAllRolesForUser(subject)).To(Succeed())
					Expect(DefaultContext.DB().Delete(person).Error).To(Succeed())
				})

				e := echo.New()
				req := httptest.NewRequest(http.MethodGet, "/db/"+table, nil)
				req = req.WithContext(DefaultContext.WithUser(person).WithSubject(subject))
				rec := httptest.NewRecorder()
				handler := mcRBAC.DbMiddleware()(func(c echo.Context) error { return c.NoContent(http.StatusOK) })
				Expect(handler(e.NewContext(req, rec))).To(Succeed())
				return rec.Code
			}

			Expect(listStatus("read-every-canary", "canaries")).To(Equal(http.StatusOK))
			Expect(listStatus("read-every-canary", "checks")).To(Equal(http.StatusForbidden), "a read of canaries doesn't cover checks")
			Expect(listStatus("read-every-check", "checks")).To(Equal(http.StatusOK))
			for _, view := range []string{"check_summary", "checks_labels_keys"} {
				Expect(listStatus("read-every-canary", view)).To(Equal(http.StatusOK), "the Health page's views of checks follow canaries: %s", view)
			}
		})

		It("filters rows of canaries, which casbin requests never carry", func() {
			canaries := newScope("canaries", namespace,
				v1.ScopeTarget{Canary: &v1.ScopeCanarySelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "http-check"}}})
			Expect(db.PersistScopeFromCRD(DefaultContext, canaries)).To(Succeed())
			role := newRole("read-canaries", namespace, allow("read", policy.ActionRead, "canaries"))
			persistRole(role)
			guest := setup.CreateUserWithRole(DefaultContext, "Role Canary Guest", "role-canary@test.com")
			binding := newBinding("read-canaries", role.Name, people(guest))
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).To(Succeed())
			DeferCleanup(func() {
				Expect(DefaultContext.DB().Where("id = ?", binding.UID).Delete(&models.RoleBinding{}).Error).To(Succeed())
				Expect(DefaultContext.DB().Delete(guest).Error).To(Succeed())
				Expect(rbac.ReloadPolicy()).To(Succeed())
			})
			Expect(rbac.ReloadPolicy()).To(Succeed())

			payload, err := auth.GetRLSPayload(DefaultContext.WithUser(guest))
			Expect(err).ToNot(HaveOccurred())
			Expect(payload.Canary).To(Equal(grant("canaries")))
			Expect(canRead(guest.ID.String(), configTagged(nil))).To(BeFalse())
		})
	})

	Describe("access summary", func() {
		summaryOf := func(person *models.Person, action string) map[string]mcRBAC.Access {
			GinkgoHelper()
			mcRBAC.FlushAccessSummaries()
			summary := map[string]mcRBAC.Access{}
			for resourceType, actions := range mcRBAC.AccessSummary(DefaultContext.WithUser(person)) {
				summary[resourceType] = actions[action]
			}
			return summary
		}

		readSummaryOf := func(person *models.Person) map[string]mcRBAC.Access {
			GinkgoHelper()
			return summaryOf(person, policy.ActionRead)
		}

		only := func(resourceType string, access mcRBAC.Access) map[string]mcRBAC.Access {
			summary := map[string]mcRBAC.Access{
				policy.ResourceConfig:     mcRBAC.AccessNone,
				policy.ResourceComponent:  mcRBAC.AccessNone,
				policy.ResourceCanary:     mcRBAC.AccessNone,
				policy.ResourcePlaybook:   mcRBAC.AccessNone,
				policy.ResourceConnection: mcRBAC.AccessNone,
			}
			summary[resourceType] = access
			return summary
		}

		createPermission := func(permission *models.Permission) {
			GinkgoHelper()
			permission.ID = uuid.New()
			permission.Namespace = "default"
			permission.SubjectType = models.PermissionSubjectTypePerson
			Expect(DefaultContext.DB().Create(permission).Error).To(Succeed())
			DeferCleanup(func() {
				Expect(DefaultContext.DB().Delete(permission).Error).To(Succeed())
				Expect(rbac.ReloadPolicy()).To(Succeed())
			})
			Expect(rbac.ReloadPolicy()).To(Succeed())
		}

		bindGuest := func(name, role string, constraint string) *models.Person {
			GinkgoHelper()
			guest := setup.CreateUserWithRole(DefaultContext, "Role Summary "+name, "role-summary-"+name+"@test.com")
			binding := newBinding("summary-"+name, role, people(guest))
			if constraint != "" {
				binding = constrained(binding, constraint, "")
			}
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).To(Succeed())
			DeferCleanup(func() {
				Expect(DefaultContext.DB().Where("id = ?", binding.UID).Delete(&models.RoleBinding{}).Error).To(Succeed())
				Expect(DefaultContext.DB().Delete(guest).Error).To(Succeed())
				Expect(rbac.ReloadPolicy()).To(Succeed())
			})
			Expect(rbac.ReloadPolicy()).To(Succeed())
			return guest
		}

		It("gives a guest some of a type its read grants filter by row", func() {
			Expect(readSummaryOf(rlsGuest)).To(Equal(only(policy.ResourceConfig, mcRBAC.AccessSome)))
		})

		It("gives some for grants on a Scope that matches nothing", func() {
			Expect(db.PersistScopeFromCRD(DefaultContext, newScope("no-configs", namespace,
				v1.ScopeTarget{Config: &v1.ScopeConfigSelector{TagSelector: "summary=nothing"}}))).To(Succeed())
			persistRole(newRole("read-no-configs", namespace, allow("read", policy.ActionRead, "no-configs")))
			guest := bindGuest("empty", "read-no-configs", "")
			Expect(readSummaryOf(guest)).To(Equal(only(policy.ResourceConfig, mcRBAC.AccessSome)))
		})

		It("gives a guest without grants none of every type", func() {
			Expect(readSummaryOf(erin)).To(Equal(only(policy.ResourceConfig, mcRBAC.AccessNone)))
		})

		It("gives a guest all of a type it's granted whole", func() {
			persistRole(newRole("read-all-configs", namespace, allow("read", policy.ActionRead, "all-configs")))
			guest := bindGuest("whole", "read-all-configs", "")
			Expect(readSummaryOf(guest)).To(Equal(only(policy.ResourceConfig, mcRBAC.AccessAll)))
		})

		It("gives some of a whole-type grant a constraint narrows", func() {
			persistRole(newRole("read-all-configs-narrowed", namespace, allow("read", policy.ActionRead, "all-configs")))
			guest := bindGuest("narrowed", "read-all-configs-narrowed", "tenant-a-boundary")
			Expect(readSummaryOf(guest)).To(Equal(only(policy.ResourceConfig, mcRBAC.AccessSome)))
		})

		It("gives people bound to the shipped viewer and admin Roles all of the types their Role grants", func() {
			viewer := newViewer("Role Summary viewer", "role-summary-viewer@test.com")
			admin := newAdmin("Role Summary admin", "role-summary-admin@test.com")

			Expect(readSummaryOf(viewer)).To(Equal(map[string]mcRBAC.Access{
				policy.ResourceConfig:     mcRBAC.AccessAll,
				policy.ResourceComponent:  mcRBAC.AccessAll,
				policy.ResourceCanary:     mcRBAC.AccessAll,
				policy.ResourcePlaybook:   mcRBAC.AccessAll,
				policy.ResourceConnection: mcRBAC.AccessNone,
			}))
			Expect(summaryOf(viewer, policy.ActionUpdate)).To(HaveEach(mcRBAC.AccessNone))

			mcRBAC.FlushAccessSummaries()
			for _, actions := range mcRBAC.AccessSummary(DefaultContext.WithUser(admin)) {
				Expect(actions).To(HaveEach(mcRBAC.AccessAll))
			}
		})

		It("gives some of a write a Permission grants on selected resources", func() {
			viewer := newViewer("Role Summary scoped-writer", "role-summary-scoped-writer@test.com")
			createPermission(&models.Permission{
				Name:           "summary-update-media-configs",
				Action:         policy.ActionUpdate,
				Subject:        viewer.ID.String(),
				ObjectSelector: []byte(`{"configs":[{"tagSelector":"namespace=media"}]}`),
			})

			Expect(summaryOf(viewer, policy.ActionUpdate)).To(Equal(only(policy.ResourceConfig, mcRBAC.AccessSome)))
			Expect(summaryOf(viewer, policy.ActionCreate)).To(HaveEach(mcRBAC.AccessNone))
		})

		It("gives all of a write granted on the whole type", func() {
			viewer := newViewer("Role Summary whole-writer", "role-summary-whole-writer@test.com")
			createPermission(&models.Permission{
				Name:    "summary-update-catalog",
				Action:  policy.ActionUpdate,
				Subject: viewer.ID.String(),
				Object:  policy.ObjectCatalog,
			})

			Expect(summaryOf(viewer, policy.ActionUpdate)).To(Equal(only(policy.ResourceConfig, mcRBAC.AccessAll)))
		})

		It("doesn't count a Permission that names several types towards any of them", func() {
			viewer := newViewer("Role Summary multi-type", "role-summary-multi-type@test.com")
			createPermission(&models.Permission{
				Name:           "summary-update-configs-with-playbook",
				Action:         policy.ActionUpdate,
				Subject:        viewer.ID.String(),
				ObjectSelector: []byte(`{"configs":[{"name":"*"}],"playbooks":[{"name":"echo-config"}]}`),
			})

			Expect(summaryOf(viewer, policy.ActionUpdate)).To(HaveEach(mcRBAC.AccessNone))
		})

		It("reads a selector whatever its text contains", func() {
			viewer := newViewer("Role Summary search-deny", "role-summary-search-deny@test.com")
			createPermission(&models.Permission{
				Name:           "summary-update-search",
				Action:         policy.ActionUpdate,
				Subject:        viewer.ID.String(),
				ObjectSelector: []byte(`{"configs":[{"search":"a && b"}]}`),
			})

			Expect(summaryOf(viewer, policy.ActionUpdate)[policy.ResourceConfig]).To(Equal(mcRBAC.AccessSome))
		})
	})
})
