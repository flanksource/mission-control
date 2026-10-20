package adapter

import (
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/flanksource/duty/types"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	v1 "github.com/flanksource/incident-commander/api/v1"
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

		invoke, err := ContractFor("invoke:kubernetes-logs:tail")
		Expect(err).ToNot(HaveOccurred())
		Expect(invoke.Resources).To(ConsistOf(policy.ResourceConfig))
	})

	for _, action := range []string{"playbook:read", "playbook:*", "*", "connection:*", "person:read", "invoke:kubernetes-logs:*", "invoke:kubernetes-logs", "plugin-role::admin"} {
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
		{"config tags", policy.ResourceConfig, types.ResourceSelector{TagSelector: "env=prod"}, true},
		{"component labels", policy.ResourceComponent, types.ResourceSelector{LabelSelector: "team=payments"}, true},
		{"playbook namespace", policy.ResourcePlaybook, types.ResourceSelector{Namespace: "operations"}, true},
		{"wildcard", policy.ResourceView, types.ResourceSelector{Name: "*"}, true},
		{"empty", policy.ResourceConfig, types.ResourceSelector{}, false},
		{"name prefix", policy.ResourceConfig, types.ResourceSelector{Name: "nginx-*"}, true},
		{"name prefix with a second wildcard", policy.ResourceConfig, types.ResourceSelector{Name: "nginx-*-*"}, false},
		{"lone wildcard prefix", policy.ResourceConfig, types.ResourceSelector{Name: "**"}, false},
		{"name with a comma, which is literal", policy.ResourceConfig, types.ResourceSelector{Name: "a,b"}, true},
		{"uppercase id", policy.ResourceConfig, types.ResourceSelector{ID: "3C3E0B2A-9F1E-4D2B-8C5A-1E2F3A4B5C6D"}, false},
		{"agent name, resolved when the scope is validated", policy.ResourceConfig, types.ResourceSelector{Agent: "homelab"}, true},
		{"name suffix", policy.ResourceConfig, types.ResourceSelector{Name: "*-prod"}, false},
		{"name starting with !, which is literal", policy.ResourceConfig, types.ResourceSelector{Name: "!api"}, true},
		{"namespace with a comma, which is literal", policy.ResourceComponent, types.ResourceSelector{Namespace: "a,b"}, true},
		{"wildcard namespace", policy.ResourceComponent, types.ResourceSelector{Namespace: "*"}, false},
		{"namespace prefix", policy.ResourceComponent, types.ResourceSelector{Namespace: "prod-*"}, false},
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
		Expect(update.validateInput("resource", views)).ToNot(Succeed())
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
			policy.ResourceConfig: {Scope: production.ID, Constraints: []string{tenantA.ID}},
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
	})
})

var _ = ginkgo.Describe("Actions without a resource", func() {
	resourceless := map[string]string{
		ActionPersonManage:          policy.ObjectPeople,
		policy.ActionMCPUse:         policy.ObjectMCP,
		policy.ActionPropertyManage: policy.ObjectProperties,
		policy.ActionEventRead:      policy.ObjectEvents,
		policy.ActionJobRead:        policy.ObjectJobs,
	}

	rule := func(name, action string, resource *v1.ScopeReference) v1.RoleRule {
		return v1.RoleRule{Name: name, Action: action, Resource: resource}
	}

	for action, object := range resourceless {
		ginkgo.It(action+" is checked on "+object, func() {
			contract, err := ContractFor(action)
			Expect(err).ToNot(HaveOccurred())
			Expect(contract.TakesResource()).To(BeFalse())
			Expect(contract.Resources).To(BeEmpty())
			Expect(contract.Targets).To(BeEmpty())
			Expect(contract.Object).To(Equal(object))
		})

		ginkgo.It(action+" is accepted without a resource", func() {
			Expect(ValidateRoleSpec("ops", []v1.RoleRule{rule("ops", action, nil)})).To(Succeed())

			resolved, err := ResolveRoleRules(context.New(), nil, "default", []v1.RoleRule{rule("ops", action, nil)})
			Expect(err).ToNot(HaveOccurred())
			Expect(resolved).To(HaveLen(1))
			Expect(resolved[0].Resource).To(BeNil())
		})

		ginkgo.It(action+" is rejected with a resource", func() {
			err := ValidateRoleSpec("ops", []v1.RoleRule{rule("ops", action, &v1.ScopeReference{ScopeRef: "everything"})})
			Expect(err).To(HaveOccurred())
			Expect(IsValidationError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("takes no resource"))
		})

		ginkgo.It(action+" is rejected with a target or as a deny", func() {
			withTarget := rule("ops", action, nil)
			withTarget.Target = &v1.ScopeReference{ScopeRef: "configs"}
			Expect(ValidateRoleSpec("ops", []v1.RoleRule{withTarget})).ToNot(Succeed())

			deny := rule("ops", action, nil)
			deny.Deny = true
			Expect(ValidateRoleSpec("ops", []v1.RoleRule{deny})).ToNot(Succeed())
		})
	}

	ginkgo.It("accepts at most v1.MaxRoleRules rules", func() {
		rules := func(n int) []v1.RoleRule {
			return lo.Times(n, func(i int) v1.RoleRule { return rule(fmt.Sprintf("rule-%d", i), policy.ActionMCPUse, nil) })
		}
		Expect(ValidateRoleSpec("ops", rules(v1.MaxRoleRules))).To(Succeed())

		err := ValidateRoleSpec("ops", rules(v1.MaxRoleRules+1))
		Expect(IsValidationError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("at most 200 rules"))
	})

	ginkgo.It("rejects an action that takes a resource without one", func() {
		for _, action := range []string{policy.ActionRead, policy.ActionUpdate, policy.ActionPlaybookRun, "invoke:kubernetes-logs:tail"} {
			err := ValidateRoleSpec("ops", []v1.RoleRule{rule("ops", action, nil)})
			Expect(err).To(HaveOccurred(), action)
			Expect(err.Error()).To(ContainSubstring("needs a resource"), action)
		}
	})

	ginkgo.It("compiles mcp:use to the policy the MCP server checks", func() {
		contract, _ := ContractFor(policy.ActionMCPUse)
		policies, err := compiledRuleToCasbinRules("binding:default/ops", CompiledRule{ID: "binding:1/mcp", Name: "mcp", Contract: contract})
		Expect(err).ToNot(HaveOccurred())
		Expect(policies).To(Equal([][]string{{"p", "binding:default/ops", policy.ObjectMCP, policy.ActionMCPUse, "allow", "", "binding:1/mcp"}}))
	})

	ginkgo.It("compiles to a single policy on the action's object, with no condition", func() {
		contract, _ := ContractFor(policy.ActionEventRead)
		policies, err := compiledRuleToCasbinRules("binding:default/ops", CompiledRule{ID: "binding:1/events", Name: "events", Contract: contract})
		Expect(err).ToNot(HaveOccurred())
		Expect(policies).To(Equal([][]string{{"p", "binding:default/ops", policy.ObjectEvents, policy.ActionEventRead, "allow", "", "binding:1/events"}}))
		Expect(CompiledRule{Contract: contract}.ReadGrants()).To(BeEmpty())
	})

	ginkgo.It("never applies through a binding with a constraint", func() {
		contract, _ := ContractFor(policy.ActionJobRead)
		resolved := ResolvedRule{RoleRule: rule("jobs", policy.ActionJobRead, nil), Contract: contract}
		configs := ScopeSelection{ID: "00000000-0000-4000-8000-000000000003", Name: "configs", Selectors: map[string][]types.ResourceSelector{
			policy.ResourceConfig: {{Name: "*"}},
		}}

		for _, scopes := range []constraintScopes{{resource: &configs}, {target: &configs}} {
			_, err := narrowRule(context.New(), CompiledRule{Name: "jobs", Contract: contract}, resolved, scopes)
			Expect(err).To(HaveOccurred())
			Expect(InvalidReason(err)).To(Equal(ReasonConstraintDoesNotFit))
			Expect(err.Error()).To(ContainSubstring("takes no resource"))
		}
	})
})

var _ = ginkgo.Describe("connection:use", func() {
	use, _ := ContractFor(ActionConnectionUse)
	read, _ := ContractFor(policy.ActionRead)

	aws := ScopeSelection{ID: "00000000-0000-4000-8000-000000000004", Name: "aws", Selectors: map[string][]types.ResourceSelector{
		policy.ResourceConnection: {{Namespace: "aws"}},
	}}
	allConnections := ScopeSelection{ID: "00000000-0000-4000-8000-000000000005", Name: "all-connections", Selectors: map[string][]types.ResourceSelector{
		policy.ResourceConnection: {{Name: "*"}},
	}}
	configs := scope("configs", map[string][]types.ResourceSelector{policy.ResourceConfig: {{Name: "*"}}})

	rule := func(name string, resource *v1.ScopeReference) v1.RoleRule {
		return v1.RoleRule{Name: name, Action: ActionConnectionUse, Resource: resource}
	}

	ginkgo.It("takes connections as its resource, and no target", func() {
		Expect(use.TakesResource()).To(BeTrue())
		Expect(use.Resources).To(ConsistOf(policy.ResourceConnection))
		Expect(use.Targets).To(BeEmpty())
		Expect(use.Object).To(BeEmpty())
	})

	ginkgo.It("accepts any connection selector, since it's checked per request", func() {
		Expect(use.validateInput("resource", aws)).To(Succeed())
		Expect(use.validateInput("resource", allConnections)).To(Succeed())
		Expect(read.validateInput("resource", aws)).ToNot(Succeed(), "read still needs whole-type connections")
	})

	ginkgo.It("rejects a scope that selects anything but connections", func() {
		Expect(use.validateInput("resource", configs)).ToNot(Succeed())
		Expect(use.validateInput("resource", scope("mixed", map[string][]types.ResourceSelector{
			policy.ResourceConnection: {{Name: "*"}},
			policy.ResourceConfig:     {{Name: "*"}},
		}))).ToNot(Succeed())
	})

	ginkgo.It("is rejected without a resource, with a target, or as a deny", func() {
		err := ValidateRoleSpec("ops", []v1.RoleRule{rule("ops", nil)})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("needs a resource"))

		withTarget := rule("ops", &v1.ScopeReference{ScopeRef: "aws"})
		withTarget.Target = &v1.ScopeReference{ScopeRef: "configs"}
		err = ValidateRoleSpec("ops", []v1.RoleRule{withTarget})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("doesn't take a target"))

		deny := rule("ops", &v1.ScopeReference{ScopeRef: "aws"})
		deny.Deny = true
		err = ValidateRoleSpec("ops", []v1.RoleRule{deny})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("deny isn't supported"))

		Expect(ValidateRoleSpec("ops", []v1.RoleRule{rule("ops", &v1.ScopeReference{ScopeRef: "aws"})})).To(Succeed())
	})

	ginkgo.It("compiles to one policy matched by the connection's scopes, and none on the connection object", func() {
		policies, err := compiledRuleToCasbinRules("binding:default/ops", CompiledRule{ID: "binding:1/use", Name: "use", Contract: use, Resource: []ScopeSelection{allConnections}})
		Expect(err).ToNot(HaveOccurred())
		Expect(policies).To(Equal([][]string{{"p", "binding:default/ops", "*", ActionConnectionUse, "allow",
			"r.obj.Membership.Fits && !r.obj.Membership.HasTarget && 'scope:" + allConnections.ID + "' in r.obj.Membership.Resource",
			"binding:1/use"}}))
		Expect(CompiledRule{Contract: use, Resource: []ScopeSelection{allConnections}}.ReadGrants()).To(BeEmpty())
	})

	ginkgo.It("is narrowed by a constraint that selects connections", func() {
		resolved := ResolvedRule{RoleRule: rule("use", &v1.ScopeReference{ScopeRef: "all-connections"}), Contract: use, Resource: &allConnections}
		compiled := CompiledRule{Name: "use", Contract: use, Resource: []ScopeSelection{allConnections}}

		narrowed, err := narrowRule(context.New(), compiled, resolved, constraintScopes{resource: &aws})
		Expect(err).ToNot(HaveOccurred())
		Expect(narrowed.Resource).To(HaveLen(2))
		Expect(narrowed.Resource[1].Selectors).To(Equal(aws.Selectors))

		_, err = narrowRule(context.New(), compiled, resolved, constraintScopes{resource: &configs})
		Expect(err).To(HaveOccurred())
		Expect(InvalidReason(err)).To(Equal(ReasonConstraintDoesNotFit))
	})
})

var _ = ginkgo.Describe("Whole-type resources", func() {
	read, _ := ContractFor(policy.ActionRead)

	wholeType := func(kind string) ScopeSelection {
		return ScopeSelection{ID: "00000000-0000-4000-8000-000000000006", Name: "all-" + kind, Selectors: map[string][]types.ResourceSelector{
			kind: {{Name: "*"}},
		}}
	}

	ginkgo.It("gives every type its own object", func() {
		objects := lo.Values(objectForType)
		Expect(lo.Uniq(objects)).To(HaveLen(len(objects)))
		for _, kind := range v1.WholeTypeResources {
			Expect(objectForType).To(HaveKey(kind))
		}
	})

	ginkgo.It("takes every type for read", func() {
		Expect(read.Resources).To(ConsistOf(append([]string{
			policy.ResourceConfig, policy.ResourceComponent, policy.ResourceCheck, policy.ResourceCanary, policy.ResourcePlaybook, policy.ResourceConnection,
			policy.ResourceScraper, policy.ResourceView,
		}, v1.WholeTypeResources...)))
	})

	for _, action := range []string{policy.ActionCreate, policy.ActionUpdate, policy.ActionDelete} {
		ginkgo.It("takes every type but checks, people and views for "+action, func() {
			contract, _ := ContractFor(action)
			Expect(contract.Resources).To(ConsistOf(append([]string{
				policy.ResourceConfig, policy.ResourceComponent, policy.ResourceCanary, policy.ResourcePlaybook, policy.ResourceConnection,
				policy.ResourceScraper,
			}, lo.Without(v1.WholeTypeResources, v1.ResourcePerson)...)))

			Expect(contract.validateInput("resource", wholeType(v1.ResourcePerson))).To(MatchError(ContainSubstring("selects person")))
			Expect(contract.validateInput("resource", wholeType(v1.ResourceTeam))).To(Succeed())
		})
	}

	for _, kind := range v1.WholeTypeResources {
		ginkgo.It("compiles read on all of "+kind+" to its object only", func() {
			scope := wholeType(kind)
			Expect(read.validateInput("resource", scope)).To(Succeed())

			policies, err := compiledRuleToCasbinRules("binding:default/ops", CompiledRule{ID: "binding:1/read", Name: "read", Contract: read, Resource: []ScopeSelection{scope}})
			Expect(err).ToNot(HaveOccurred())
			Expect(policies).To(Equal([][]string{
				{"p", "binding:default/ops", "*", policy.ActionRead, "allow",
					"r.obj.Membership.Fits && !r.obj.Membership.HasTarget && 'scope:" + scope.ID + "' in r.obj.Membership.Resource", "binding:1/read"},
				{"p", "binding:default/ops", objectForType[kind], policy.ActionRead, "allow", "", "binding:1/read"},
			}))
		})
	}

	ginkgo.Describe("scrapers", func() {
		oneAgent := ScopeSelection{ID: "00000000-0000-4000-8000-000000000007", Name: "homelab-scrapers", Selectors: map[string][]types.ResourceSelector{
			policy.ResourceScraper: {{Agent: "homelab"}},
		}}

		ginkgo.It("filters listings by row, so read takes any scraper Scope", func() {
			Expect(read.validateInput("resource", oneAgent)).To(Succeed())
			Expect(CompiledRule{Contract: read, Resource: []ScopeSelection{oneAgent}}.ReadGrants()).To(Equal(map[string]rls.Grant{
				policy.ResourceScraper: {Scope: oneAgent.ID},
			}))
		})

		for _, action := range []string{policy.ActionCreate, policy.ActionUpdate, policy.ActionDelete} {
			ginkgo.It("only takes all scrapers for "+action, func() {
				contract, _ := ContractFor(action)
				Expect(contract.validateInput("resource", wholeType(policy.ResourceScraper))).To(Succeed())
				Expect(contract.validateInput("resource", oneAgent)).To(MatchError(ContainSubstring(`exactly name: "*"`)))
			})
		}

		ginkgo.It("compiles a whole-type rule to the scrapers object", func() {
			update, _ := ContractFor(policy.ActionUpdate)
			policies, err := compiledRuleToCasbinRules("binding:default/ops", CompiledRule{ID: "binding:1/update", Name: "update", Contract: update, Resource: []ScopeSelection{wholeType(policy.ResourceScraper)}})
			Expect(err).ToNot(HaveOccurred())
			Expect(policies).To(ContainElement([]string{"p", "binding:default/ops", ObjectScrapers, policy.ActionUpdate, "allow", "", "binding:1/update"}))
		})
	})
})

var _ = ginkgo.Describe("Deny rules", func() {
	for _, action := range Actions() {
		ginkgo.It("are refused on "+action, func() {
			contract, err := ContractFor(action)
			Expect(err).ToNot(HaveOccurred())

			rule := v1.RoleRule{Name: "ops", Action: action, Deny: true}
			if contract.TakesResource() {
				rule.Resource = &v1.ScopeReference{ScopeRef: "everything"}
			}

			err = ValidateRoleSpec("ops", []v1.RoleRule{rule})
			Expect(err).To(HaveOccurred())
			Expect(InvalidReason(err)).To(Equal(ReasonInvalid))
			Expect(err.Error()).To(ContainSubstring("deny isn't supported"))

			rule.Deny = false
			Expect(ValidateRoleSpec("ops", []v1.RoleRule{rule})).To(Succeed())
		})
	}
})
