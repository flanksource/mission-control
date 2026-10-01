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
		{"playbook category", policy.ResourcePlaybook, types.ResourceSelector{FieldSelector: "category=Kubernetes", Namespace: "operations"}, true},
		{"wildcard", policy.ResourceView, types.ResourceSelector{Name: "*"}, true},
		{"empty", policy.ResourceConfig, types.ResourceSelector{}, false},
		{"component tags", policy.ResourceComponent, types.ResourceSelector{TagSelector: "team=payments"}, false},
		{"playbook tags", policy.ResourcePlaybook, types.ResourceSelector{TagSelector: "purpose=remediation"}, false},
		{"playbook title", policy.ResourcePlaybook, types.ResourceSelector{FieldSelector: "title=Restart"}, false},
		{"name prefix", policy.ResourceConfig, types.ResourceSelector{Name: "nginx-*"}, false},
		{"name list", policy.ResourceConfig, types.ResourceSelector{Name: "a,b"}, false},
		{"uppercase id", policy.ResourceConfig, types.ResourceSelector{ID: "3C3E0B2A-9F1E-4D2B-8C5A-1E2F3A4B5C6D"}, false},
		{"agent name, resolved when the scope is validated", policy.ResourceConfig, types.ResourceSelector{Agent: "homelab"}, true},
		{"name suffix", policy.ResourceConfig, types.ResourceSelector{Name: "*-prod"}, false},
		{"name exclusion", policy.ResourceConfig, types.ResourceSelector{Name: "!api"}, false},
		{"wildcard namespace", policy.ResourceConfig, types.ResourceSelector{Namespace: "*"}, false},
		{"namespace prefix", policy.ResourceConfig, types.ResourceSelector{Namespace: "prod-*"}, false},
		{"wildcard id", policy.ResourceConfig, types.ResourceSelector{ID: "*"}, false},
		{"query option", policy.ResourceConfig, types.ResourceSelector{Name: "api", Limit: 10}, false},
		{"type pattern", policy.ResourceConfig, types.ResourceSelector{Types: []string{"Kubernetes::*"}}, false},
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

	ginkgo.It("rejects scopes with global targets", func() {
		Expect(read.validateInput("resource", ScopeSelection{Name: "global", Global: true})).ToNot(Succeed())
	})

	ginkgo.It("rejects read selectors row filters can't enforce", func() {
		Expect(read.validateInput("resource", tenantA)).To(Succeed())
		Expect(read.validateInput("resource", targets)).ToNot(Succeed())
	})

	ginkgo.It("only accepts whole types for actions checked on object types", func() {
		Expect(update.validateInput("resource", allConfigs)).To(Succeed())
		Expect(update.validateInput("resource", tenantA)).ToNot(Succeed())
	})
})

var _ = ginkgo.Describe("RowFilter", func() {
	ginkgo.It("matches a config's namespace as its namespace tag", func() {
		filter, err := RowFilter(policy.ResourceConfig, types.ResourceSelector{Namespace: "staging", TagSelector: "team=payments"})
		Expect(err).ToNot(HaveOccurred())
		Expect(*filter).To(Equal(rls.Scope{Tags: map[string]string{"namespace": "staging", "team": "payments"}}))
	})

	ginkgo.It("matches every row for a wildcard", func() {
		filter, err := RowFilter(policy.ResourceConfig, types.ResourceSelector{Name: "*"})
		Expect(err).ToNot(HaveOccurred())
		Expect(*filter).To(Equal(rls.Scope{ID: "*"}))
	})

	ginkgo.It("matches no row when the namespace contradicts the namespace tag", func() {
		filter, err := RowFilter(policy.ResourceConfig, types.ResourceSelector{Namespace: "staging", TagSelector: "namespace=production"})
		Expect(err).ToNot(HaveOccurred())
		Expect(filter).To(BeNil())
	})

	ginkgo.It("filters checks by name", func() {
		filter, err := RowFilter(policy.ResourceCheck, types.ResourceSelector{Name: "http"})
		Expect(err).ToNot(HaveOccurred())
		Expect(*filter).To(Equal(rls.Scope{Names: []string{"http"}}))
	})

	ginkgo.It("rejects what row filters can't match", func() {
		for kind, selector := range map[string]types.ResourceSelector{
			policy.ResourceComponent:  {Namespace: "staging"},
			policy.ResourcePlaybook:   {FieldSelector: "category=Kubernetes"},
			policy.ResourceCheck:      {Namespace: "staging"},
			policy.ResourceConnection: {Name: "aws"},
		} {
			_, err := RowFilter(kind, selector)
			Expect(err).To(HaveOccurred(), kind)
		}

		_, err := RowFilter(policy.ResourceConfig, types.ResourceSelector{TagSelector: "env!=prod"})
		Expect(err).To(HaveOccurred())
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

var _ = ginkgo.Describe("CompiledRule.RowFilters", func() {
	ginkgo.It("filters by the intersection of every scope of the rule", func() {
		rule := CompiledRule{Resource: []ScopeSelection{
			{Name: "production", Selectors: map[string][]types.ResourceSelector{
				policy.ResourceConfig:    {{TagSelector: "env=production"}},
				policy.ResourceComponent: {{Name: "api"}},
			}},
			{Name: "tenant-a", Selectors: map[string][]types.ResourceSelector{
				policy.ResourceConfig: {{TagSelector: "tenant=a"}, {TagSelector: "tenant=shared"}},
			}},
		}}

		filters, err := rule.RowFilters()
		Expect(err).ToNot(HaveOccurred())
		Expect(filters).To(Equal(map[string][]rls.Scope{
			policy.ResourceConfig: {
				{Tags: map[string]string{"env": "production", "tenant": "a"}},
				{Tags: map[string]string{"env": "production", "tenant": "shared"}},
			},
		}), "tenant-a selects no components, so no component rows are left")
	})
})
