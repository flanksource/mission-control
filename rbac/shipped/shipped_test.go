package shipped

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
		"p":               {"x-y", "x-y-0ccfea77", "x_y"},
	}

	ginkgo.BeforeEach(func() {
		objects, err := Build(operations)
		Expect(err).ToNot(HaveOccurred())
		shippedScopes, shippedRoles := objects.Scopes, objects.Roles

		scopes = map[string]adapter.ScopeSelection{}
		for _, scope := range shippedScopes {
			Expect(scope.Namespace).To(Equal(adapter.ShippedNamespace))
			Expect(adapter.ValidateScopeTargets(scope.Spec.Targets)).To(Succeed(), scope.Name)
			scopes[scope.Name] = adapter.NewScopeSelection("", scope.Name, scope.Spec.Targets)
		}

		roles = lo.KeyBy(shippedRoles, func(r v1.Role) string { return r.Name })
		Expect(lo.Keys(roles)).To(ConsistOf(RoleViewer, RoleAdmin, RoleAccessManage, RoleKubernetesProxy, RolePluginAdmin))
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

	ginkgo.It("ships one Scope per type, each selecting exactly that type", func() {
		// Roles anyone creates may use these Scopes, so a Scope never changes what it selects: a new type gets a new Scope
		Expect(lo.MapValues(scopes, func(scope adapter.ScopeSelection, _ string) []string { return scope.DeclaredTypes() })).To(Equal(map[string][]string{
			"all-configs":               {policy.ResourceConfig},
			"all-components":            {policy.ResourceComponent},
			"all-checks":                {policy.ResourceCheck},
			"all-canaries":              {policy.ResourceCanary},
			"all-playbooks":             {policy.ResourcePlaybook},
			"all-connections":           {policy.ResourceConnection},
			"all-scrapers":              {policy.ResourceScraper},
			"all-views":                 {policy.ResourceView},
			"all-applications":          {v1.ResourceApplication},
			"all-notifications":         {v1.ResourceNotification},
			"all-notification-silences": {v1.ResourceNotificationSilence},
			"all-agents":                {v1.ResourceAgent},
			"all-people":                {v1.ResourcePerson},
			"all-teams":                 {v1.ResourceTeam},
			"all-scopes":                {v1.ResourceScope},
			"all-roles":                 {v1.ResourceRole},
			"all-role-bindings":         {v1.ResourceRoleBinding},
		}))
		for name, scope := range scopes {
			Expect(scope.Selectors).To(HaveLen(1), name)
			for _, selectors := range scope.Selectors {
				Expect(selectors).To(HaveLen(1), name)
			}
		}
	})

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
			policy.ResourcePlaybook, policy.ResourceView, v1.ResourceApplication, v1.ResourcePerson, v1.ResourceTeam,
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

	ginkgo.It("keeps admin as written, whatever plugins are installed", func() {
		without, err := Build(nil)
		Expect(err).ToNot(HaveOccurred())
		withoutAdmin, _ := lo.Find(without.Roles, func(r v1.Role) bool { return r.Name == RoleAdmin })
		Expect(roles[RoleAdmin]).To(Equal(withoutAdmin))

		for _, rule := range roles[RoleAdmin].Spec.Rules {
			Expect(rule.Action).ToNot(HavePrefix(policy.ActionPluginInvokePrefix))
		}
	})

	ginkgo.It("ships no plugin-admin, nor a binding to it, without a plugin operation", func() {
		without, err := Build(map[string][]string{"bad": {"has space"}, "empty": nil})
		Expect(err).ToNot(HaveOccurred())
		Expect(lo.Map(without.Roles, func(r v1.Role, _ int) string { return r.Name })).To(ConsistOf(RoleViewer, RoleAdmin, RoleAccessManage, RoleKubernetesProxy))
		Expect(lo.Map(without.RoleBindings, func(b v1.RoleBinding, _ int) string { return b.Name })).To(ConsistOf(BindingAdmins))

		with, err := Build(operations)
		Expect(err).ToNot(HaveOccurred())
		Expect(lo.Map(with.RoleBindings, func(b v1.RoleBinding, _ int) string { return b.Name })).To(ConsistOf(BindingAdmins, BindingPluginAdmins))
	})

	ginkgo.It("gives plugin-admin one rule per valid plugin operation, and nothing else", func() {
		invoke := roles[RolePluginAdmin].Spec.Rules
		for _, rule := range invoke {
			Expect(rule.Action).To(HavePrefix(policy.ActionPluginInvokePrefix))
		}
		Expect(lo.Map(invoke, func(r v1.RoleRule, _ int) string { return r.Action })).To(ConsistOf(
			policy.NewPluginInvokeAction("kubernetes-logs", "tail"),
			policy.NewPluginInvokeAction("kubernetes-logs", "Get_Pods"),
			policy.NewPluginInvokeAction("p", "x-y"),
			policy.NewPluginInvokeAction("p", "x-y-0ccfea77"),
			policy.NewPluginInvokeAction("p", "x_y"),
		))
		Expect(lo.Map(invoke, func(r v1.RoleRule, _ int) string { return r.Name })).To(ConsistOf(
			"invoke-kubernetes-logs-tail", "invoke-kubernetes-logs-get-pods",
			"invoke-p-x-y", "invoke-p-x-y-0ccfea77", "invoke-p-x-y-0ccfea77-2",
		))
		for _, rule := range invoke {
			Expect(selects(rule.Resource)).To(Equal([]string{policy.ResourceConfig}))
		}
	})

	ginkgo.It("keeps plugin rule names within 63 characters, however long their counter", func() {
		// <X>_y falls back to <X>-<hash>; operations named after that hash and its first counters push it to -1000
		stem := strings.Repeat("x", 41)
		sum := sha256.Sum256([]byte(policy.NewPluginInvokeAction("p", stem+"_y")))
		hash := hex.EncodeToString(sum[:])[:8]
		ops := []string{stem + "-y", stem + "-" + hash, stem + "_y"}
		for i := 2; i <= 999; i++ {
			ops = append(ops, fmt.Sprintf("%s-%s-%d", stem, hash, i))
		}
		slices.Sort(ops)

		rules := invokeRules(map[string][]string{"p": ops})
		Expect(rules).To(HaveLen(len(ops)))
		for _, rule := range rules {
			Expect(len(rule.Name)).To(BeNumerically("<=", 63), rule.Name)
		}
		// The shipped plugin-admin isn't limited to v1.MaxRoleRules, so its rules are checked one by one
		Expect(v1.RoleSpec{Rules: rules}.Validate()).To(Succeed())
		for _, rule := range rules {
			Expect(adapter.ValidateRoleSpec(RolePluginAdmin, []v1.RoleRule{rule})).To(Succeed(), rule.Name)
		}
	})

	ginkgo.It("binds admin and plugin-admin to the admins team, and only that", func() {
		objects, err := Load()
		Expect(err).ToNot(HaveOccurred())

		Expect(objects.Teams).To(HaveLen(1))
		Expect(objects.Teams[0].Name).To(Equal(TeamAdmins))
		Expect(objects.Teams[0].Spec.Members).To(BeEmpty())

		bindings := lo.KeyBy(objects.RoleBindings, func(b v1.RoleBinding) string { return b.Name })
		Expect(lo.Keys(bindings)).To(ConsistOf(BindingAdmins, BindingPluginAdmins))
		Expect(bindings[BindingAdmins].Spec.Role).To(Equal(RoleAdmin))
		Expect(bindings[BindingPluginAdmins].Spec.Role).To(Equal(RolePluginAdmin))
		for _, binding := range bindings {
			Expect(binding.Namespace).To(Equal(adapter.ShippedNamespace))
			Expect(binding.Spec.Validate()).To(Succeed())
			Expect(binding.Spec.Constraint).To(BeNil())
			Expect(binding.Spec.Subjects).To(Equal(v1.RoleBindingSubjects{
				PermissionGroupSubjects: v1.PermissionGroupSubjects{Teams: []string{TeamAdmins}},
			}))
		}
	})

	ginkgo.It("names plugin rules uniquely, within the length of a rule name", func() {
		used := map[string]bool{}
		long := policy.NewPluginInvokeAction(strings.Repeat("p", 40), strings.Repeat("o", 40))
		// x_y falls back to the hash of its action, invoke-p-x-y-0ccfea77, which x-y-0ccfea77 already took
		for _, action := range []string{"invoke:a:b_c", "invoke:a:b-c", long, "invoke:p:x-y", "invoke:p:x-y-0ccfea77", "invoke:p:x_y"} {
			name := invokeRuleName(action, used)
			Expect(used).ToNot(HaveKey(name))
			Expect(len(name)).To(BeNumerically("<=", 63))
			Expect(adapter.ValidateRoleSpec("test", []v1.RoleRule{{Name: name, Action: action, Resource: &v1.ScopeReference{ScopeRef: allConfigs}}})).To(Succeed())
			used[name] = true
		}
	})
})
