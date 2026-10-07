package adapter

import (
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/flanksource/duty/types"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("ContractFor", func() {
	ginkgo.It("declares the inputs of every supported action", func() {
		run, err := ContractFor(policy.ActionPlaybookRun)
		Expect(err).ToNot(HaveOccurred())
		Expect(run.Resources).To(ConsistOf(policy.ResourcePlaybook))
		Expect(run.Targets).To(ConsistOf(policy.ResourceConfig, policy.ResourceComponent, policy.ResourceCheck))

		read, err := ContractFor(policy.ActionRead)
		Expect(err).ToNot(HaveOccurred())
		Expect(read.Targets).To(BeEmpty())
		Expect(read.AllowsDeny).To(BeFalse())

		invoke, err := ContractFor("invoke:kubernetes-logs:tail")
		Expect(err).ToNot(HaveOccurred())
		Expect(invoke.Resources).To(ConsistOf(policy.ResourceConfig))
	})

	for _, action := range []string{"playbook:read", "playbook:*", "*", policy.ActionMCPUse, "invoke:kubernetes-logs:*", "invoke:kubernetes-logs", "plugin-role::admin"} {
		ginkgo.It("rejects "+action, func() {
			_, err := ContractFor(action)
			Expect(err).To(HaveOccurred())
		})
	}
})

var _ = ginkgo.Describe("ValidateSelector", func() {
	for _, tt := range []struct {
		name     string
		kind     string
		selector types.ResourceSelector
		valid    bool
	}{
		{"config tags", policy.ResourceConfig, types.ResourceSelector{TagSelector: "env=prod", Namespace: "default"}, true},
		{"component labels", policy.ResourceComponent, types.ResourceSelector{LabelSelector: "team=payments"}, true},
		{"playbook namespace", policy.ResourcePlaybook, types.ResourceSelector{Namespace: "operations"}, true},
		{"wildcard", policy.ResourceView, types.ResourceSelector{Name: "*"}, true},
		{"empty", policy.ResourceConfig, types.ResourceSelector{}, false},
		{"name prefix", policy.ResourceConfig, types.ResourceSelector{Name: "nginx-*"}, true},
		{"name prefix with a second wildcard", policy.ResourceConfig, types.ResourceSelector{Name: "nginx-*-*"}, false},
		{"lone wildcard prefix", policy.ResourceConfig, types.ResourceSelector{Name: "**"}, false},
		{"name list", policy.ResourceConfig, types.ResourceSelector{Name: "a,b"}, false},
		{"uppercase id", policy.ResourceConfig, types.ResourceSelector{ID: "3C3E0B2A-9F1E-4D2B-8C5A-1E2F3A4B5C6D"}, false},
		{"agent name, resolved when the scope is validated", policy.ResourceConfig, types.ResourceSelector{Agent: "homelab"}, true},
		{"name suffix", policy.ResourceConfig, types.ResourceSelector{Name: "*-prod"}, false},
		{"name exclusion", policy.ResourceConfig, types.ResourceSelector{Name: "!api"}, false},
		{"wildcard namespace", policy.ResourceConfig, types.ResourceSelector{Namespace: "*"}, false},
		{"namespace prefix", policy.ResourceConfig, types.ResourceSelector{Namespace: "prod-*"}, false},
		{"wildcard id", policy.ResourceConfig, types.ResourceSelector{ID: "*"}, false},
		{"type pattern", policy.ResourceConfig, types.ResourceSelector{Types: []string{"Kubernetes::*"}}, false},
		{"several tags", policy.ResourceConfig, types.ResourceSelector{TagSelector: "cluster=homelab,namespace=monitoring"}, true},
		{"tag inequality", policy.ResourceConfig, types.ResourceSelector{TagSelector: "env!=prod"}, false},
		{"tag double equals", policy.ResourceConfig, types.ResourceSelector{TagSelector: "env==prod"}, false},
		{"tag in", policy.ResourceConfig, types.ResourceSelector{TagSelector: "env in (prod,staging)"}, false},
		{"tag notin", policy.ResourceConfig, types.ResourceSelector{TagSelector: "env notin (prod)"}, false},
		{"bare tag key", policy.ResourceConfig, types.ResourceSelector{TagSelector: "env"}, false},
		{"tag exclusion", policy.ResourceConfig, types.ResourceSelector{TagSelector: "!env"}, false},
		{"label inequality", policy.ResourceComponent, types.ResourceSelector{LabelSelector: "team=payments,env!=prod"}, false},
		{"bare label key", policy.ResourceCanary, types.ResourceSelector{LabelSelector: "team"}, false},
	} {
		ginkgo.It(tt.name, func() {
			err := ValidateSelector(tt.kind, tt.selector)
			if tt.valid {
				Expect(err).ToNot(HaveOccurred())
			} else {
				Expect(err).To(HaveOccurred())
			}
		})
	}
})

func scope(name string, selectors map[string][]types.ResourceSelector) ScopeSelection {
	return ScopeSelection{Name: name, Selectors: selectors}
}

var _ = ginkgo.Describe("validateInput", func() {

	run, _ := ContractFor(policy.ActionPlaybookRun)
	read, _ := ContractFor(policy.ActionRead)
	update, _ := ContractFor(policy.ActionUpdate)

	playbooks := scope("playbooks", map[string][]types.ResourceSelector{policy.ResourcePlaybook: {{Name: "*"}}})
	configsAndPlaybooks := scope("mixed", map[string][]types.ResourceSelector{
		policy.ResourcePlaybook: {{Name: "*"}},
		policy.ResourceConfig:   {{Name: "*"}},
	})
	targets := scope("targets", map[string][]types.ResourceSelector{
		policy.ResourceConfig:    {{TagSelector: "env=staging"}},
		policy.ResourceComponent: {{Namespace: "staging"}},
		policy.ResourceCheck:     {{Namespace: "staging"}},
	})
	views := scope("views", map[string][]types.ResourceSelector{policy.ResourceView: {{Namespace: "staging"}}})
	tenantA := scope("tenant-a", map[string][]types.ResourceSelector{policy.ResourceConfig: {{TagSelector: "tenant=a"}}})
	allConfigs := scope("all-configs", map[string][]types.ResourceSelector{policy.ResourceConfig: {{Name: "*"}}})

	ginkgo.It("accepts a scope whose every declared type the input accepts", func() {
		Expect(run.validateInput("resource", playbooks)).To(Succeed())
		Expect(run.validateInput("target", targets)).To(Succeed())
	})

	ginkgo.It("rejects a scope with any type the input doesn't accept", func() {
		Expect(run.validateInput("resource", configsAndPlaybooks)).ToNot(Succeed())
		Expect(run.validateInput("target", views)).ToNot(Succeed())
		Expect(run.validateInput("target", playbooks)).ToNot(Succeed())
		Expect(read.validateInput("resource", views)).ToNot(Succeed())
	})

	ginkgo.It("accepts any read scope: membership is decided by the resource alone", func() {
		Expect(read.validateInput("resource", tenantA)).To(Succeed())
		Expect(read.validateInput("resource", targets)).To(Succeed())
	})

	ginkgo.It("only accepts whole-type connections for read, since connections aren't filtered by row", func() {
		Expect(read.validateInput("resource", scope("all-connections", map[string][]types.ResourceSelector{policy.ResourceConnection: {{Name: "*"}}}))).To(Succeed())
		Expect(read.validateInput("resource", scope("aws", map[string][]types.ResourceSelector{policy.ResourceConnection: {{Name: "aws"}}}))).ToNot(Succeed())
	})

	ginkgo.It("only accepts whole types for actions checked on object types", func() {
		Expect(update.validateInput("resource", allConfigs)).To(Succeed())
		Expect(update.validateInput("resource", tenantA)).ToNot(Succeed())
	})
})

var _ = ginkgo.Describe("row-level security requirement", func() {
	read, _ := ContractFor(policy.ActionRead)
	run, _ := ContractFor(policy.ActionPlaybookRun)

	ginkgo.It("is needed by a read rule whose scope doesn't select whole types", func() {
		Expect(read.requiresRowLevelSecurity(scope("staging", map[string][]types.ResourceSelector{
			policy.ResourceConfig: {{Name: "*"}, {TagSelector: "env=staging"}},
		}))).To(BeTrue())
	})

	ginkgo.It("isn't needed by a read rule of whole-type targets only", func() {
		Expect(read.requiresRowLevelSecurity(scope("all", map[string][]types.ResourceSelector{
			policy.ResourceConfig: {{Name: "*"}},
			policy.ResourceCheck:  {{Name: "*"}},
		}))).To(BeFalse())
	})

	ginkgo.It("isn't needed by actions checked per request", func() {
		Expect(run.requiresRowLevelSecurity(scope("monitoring", map[string][]types.ResourceSelector{
			policy.ResourcePlaybook: {{Namespace: "monitoring"}},
		}))).To(BeFalse())
	})
})

var _ = ginkgo.Describe("CompiledRule", func() {
	read, _ := ContractFor(policy.ActionRead)
	run, _ := ContractFor(policy.ActionPlaybookRun)

	production := ScopeSelection{ID: "00000000-0000-4000-8000-000000000001", Name: "production", Selectors: map[string][]types.ResourceSelector{
		policy.ResourceConfig:    {{TagSelector: "env=production"}},
		policy.ResourceComponent: {{Name: "api"}},
	}}
	tenantA := ScopeSelection{ID: "00000000-0000-4000-8000-000000000002", Name: "tenant-a", Selectors: map[string][]types.ResourceSelector{
		policy.ResourceConfig: {{TagSelector: "tenant=a"}},
	}}

	ginkgo.It("lists, by type, the rows in every scope of a read rule", func() {
		rule := CompiledRule{Contract: read, Resource: []ScopeSelection{production, tenantA}}
		Expect(rule.ReadGrants()).To(Equal(map[string]rls.Grant{
			policy.ResourceConfig: {Scope: production.ID, Constraint: tenantA.ID},
		}), "tenant-a selects no components, so no component rows are listed")

		Expect(CompiledRule{Contract: read, Resource: []ScopeSelection{production}}.ReadGrants()).To(Equal(map[string]rls.Grant{
			policy.ResourceConfig:    {Scope: production.ID},
			policy.ResourceComponent: {Scope: production.ID},
		}))
	})

	ginkgo.It("lists no rows through rules of other actions", func() {
		Expect(CompiledRule{Contract: run, Resource: []ScopeSelection{production}}.ReadGrants()).To(BeEmpty())
	})

	ginkgo.It("matches requests by the scopes of their resources", func() {
		rule := CompiledRule{Name: "read", Contract: read, Resource: []ScopeSelection{production, tenantA}}
		condition, err := rule.condition()
		Expect(err).ToNot(HaveOccurred())
		Expect(condition).To(Equal("r.obj.Membership.Fits && !r.obj.Membership.HasTarget && " +
			"'scope:" + production.ID + "' in r.obj.Membership.Resource && 'scope:" + tenantA.ID + "' in r.obj.Membership.Resource"))

		deny := CompiledRule{Name: "deny", Contract: run, Deny: true, Resource: []ScopeSelection{production}, Target: []ScopeSelection{tenantA}}
		condition, err = deny.condition()
		Expect(err).ToNot(HaveOccurred())
		Expect(condition).To(Equal("!r.obj.Membership.Fits || (r.obj.Membership.HasTarget && " +
			"'scope:" + production.ID + "' in r.obj.Membership.Resource && 'scope:" + tenantA.ID + "' in r.obj.Membership.Target)"))
	})
})
