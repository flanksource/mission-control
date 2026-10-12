package shipped

import (
	"slices"
	"strings"

	"github.com/flanksource/duty/rbac/policy"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = ginkgo.Describe("Shipped roles", func() {
	var scopes map[string]adapter.ScopeSelection
	var roles map[string]v1.Role

	operations := map[string][]string{
		"kubernetes-logs": {"tail", "Get_Pods"},
		"bad":             {"has space"},
	}

	ginkgo.BeforeEach(func() {
		shippedScopes, shippedRoles, err := Build(operations)
		Expect(err).ToNot(HaveOccurred())

		scopes = map[string]adapter.ScopeSelection{}
		for _, scope := range shippedScopes {
			Expect(scope.Namespace).To(Equal(adapter.ShippedNamespace))
			Expect(adapter.ValidateScopeTargets(scope.Spec.Targets)).To(Succeed(), scope.Name)
			scopes[scope.Name] = adapter.NewScopeSelection("", scope.Name, scope.Spec.Targets)
		}

		roles = lo.KeyBy(shippedRoles, func(r v1.Role) string { return r.Name })
		Expect(roles).To(HaveLen(2))
		for _, role := range shippedRoles {
			Expect(role.Namespace).To(Equal(adapter.ShippedNamespace))
			// Shipped Roles are the only ones that may take a built-in role's name, which ValidateRoleSpec alone refuses
			Expect(adapter.ValidateRoleSpec("shipped-"+role.Name, role.Spec.Rules)).To(Succeed(), role.Name)
		}
	})

	// selects returns the types a rule's input can reach, failing when a Scope selects anything but whole types.
	selects := func(ref *v1.ScopeReference) []string {
		ginkgo.GinkgoHelper()
		if ref == nil {
			return nil
		}
		scope, ok := scopes[ref.ScopeRef]
		Expect(ok).To(BeTrue(), "scope %s isn't shipped", ref.ScopeRef)
		for kind, selectors := range scope.Selectors {
			for _, selector := range selectors {
				Expect(selector.Wildcard()).To(BeTrue(), "scope %s selects part of %s", ref.ScopeRef, kind)
			}
		}
		return scope.DeclaredTypes()
	}

	ginkgo.It("uses only shipped Scopes", func() {
		for _, role := range roles {
			for _, rule := range role.Spec.Rules {
				selects(rule.Resource)
				selects(rule.Target)
			}
		}
	})

	ginkgo.It("lets viewer read what people operate on, and nothing else", func() {
		viewer := roles[RoleViewer]
		var read []string
		for _, rule := range viewer.Spec.Rules {
			Expect(rule.Action).To(Equal(policy.ActionRead))
			Expect(rule.Target).To(BeNil())
			read = append(read, selects(rule.Resource)...)
		}

		// overview.md, "Shipped Roles"
		wanted := []string{
			policy.ResourceConfig, policy.ResourceComponent, policy.ResourceCheck, policy.ResourceCanary,
			policy.ResourcePlaybook, v1.ResourceApplication, v1.ResourcePerson, v1.ResourceTeam,
		}
		Expect(read).To(ConsistOf(wanted))
	})

	ginkgo.It("gives admin every action on every type it accepts", func() {
		admin := roles[RoleAdmin]
		for _, action := range adapter.Actions() {
			contract, err := adapter.ContractFor(action)
			Expect(err).ToNot(HaveOccurred())
			rules := lo.Filter(admin.Spec.Rules, func(r v1.RoleRule, _ int) bool { return r.Action == action })
			Expect(rules).ToNot(BeEmpty(), "admin has no %s rule", action)

			if !contract.TakesResource() {
				continue
			}

			var resources, targets []string
			withoutTarget := false
			for _, rule := range rules {
				resources = append(resources, selects(rule.Resource)...)
				targets = append(targets, selects(rule.Target)...)
				if rule.Target == nil && slices.Equal(selects(rule.Resource), contract.Resources) {
					withoutTarget = true
				}
			}
			Expect(lo.Uniq(resources)).To(ConsistOf(contract.Resources), action)

			if len(contract.Targets) > 0 {
				Expect(lo.Uniq(targets)).To(ConsistOf(contract.Targets), action)
				Expect(withoutTarget).To(BeTrue(), "admin can't %s without a target", action)
			}
		}
	})

	ginkgo.It("gives admin one rule per valid plugin operation", func() {
		admin := roles[RoleAdmin]
		invoke := lo.Filter(admin.Spec.Rules, func(r v1.RoleRule, _ int) bool {
			return strings.HasPrefix(r.Action, policy.ActionPluginInvokePrefix)
		})
		Expect(lo.Map(invoke, func(r v1.RoleRule, _ int) string { return r.Action })).To(ConsistOf(
			policy.NewPluginInvokeAction("kubernetes-logs", "tail"),
			policy.NewPluginInvokeAction("kubernetes-logs", "Get_Pods"),
		))
		Expect(lo.Map(invoke, func(r v1.RoleRule, _ int) string { return r.Name })).To(ConsistOf(
			"invoke-kubernetes-logs-tail", "invoke-kubernetes-logs-get-pods",
		))
		for _, rule := range invoke {
			Expect(selects(rule.Resource)).To(Equal([]string{policy.ResourceConfig}))
		}
	})

	ginkgo.It("names plugin rules uniquely, within the length of a rule name", func() {
		used := map[string]bool{}
		long := policy.NewPluginInvokeAction(strings.Repeat("p", 40), strings.Repeat("o", 40))
		for _, action := range []string{"invoke:a:b_c", "invoke:a:b-c", long} {
			name := invokeRuleName(action, used)
			Expect(used).ToNot(HaveKey(name))
			Expect(len(name)).To(BeNumerically("<=", 63))
			Expect(adapter.ValidateRoleSpec("test", []v1.RoleRule{{Name: name, Action: action, Resource: &v1.ScopeReference{ScopeRef: allConfigs}}})).To(Succeed())
			used[name] = true
		}
	})
})
