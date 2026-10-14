package adapter

import (
	"github.com/flanksource/duty/rbac/membership"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = ginkgo.Describe("Scope exclusions", func() {
	playbook := func(namespace, name string) v1.ScopeTarget {
		return v1.ScopeTarget{Playbook: &v1.ScopePlaybookRef{Namespace: namespace, Name: name}}
	}
	allPlaybooks := []v1.ScopeTarget{playbook("", "*")}

	// everyPlaybookExcept is every playbook but delete-production and those in break-glass
	everyPlaybookExcept := v1.ScopeSpec{
		Targets: allPlaybooks,
		Exclude: []v1.ScopeTarget{playbook("default", "delete-production"), playbook("break-glass", "")},
	}

	ginkgo.Describe("ValidateScopeSpec", func() {
		ginkgo.It("accepts playbook exclusions by name, prefix or namespace", func() {
			Expect(ValidateScopeSpec(everyPlaybookExcept)).To(Succeed())
			Expect(ValidateScopeSpec(v1.ScopeSpec{Targets: allPlaybooks, Exclude: []v1.ScopeTarget{playbook("", "delete-*")}})).To(Succeed())
			Expect(ValidateScopeSpec(v1.ScopeSpec{Targets: []v1.ScopeTarget{playbook("ops", "")}, Exclude: []v1.ScopeTarget{playbook("", "ops-delete-*")}})).To(Succeed())
		})

		for _, tt := range []struct {
			name    string
			spec    v1.ScopeSpec
			message string
		}{
			{"an exclusion of every playbook", v1.ScopeSpec{Targets: allPlaybooks, Exclude: []v1.ScopeTarget{playbook("", "*")}}, `name: "*" excludes every playbook`},
			{"an exclusion of a type no target selects", v1.ScopeSpec{
				Targets: []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}}},
				Exclude: []v1.ScopeTarget{playbook("", "delete-production")},
			}, "no target selects playbook"},
			{"an exclusion of configs", v1.ScopeSpec{
				Targets: []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}}},
				Exclude: []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{TagSelector: "env=production"}}},
			}, "only playbook can be excluded"},
			{"an exclusion of a type only selected entirely", v1.ScopeSpec{
				Targets: []v1.ScopeTarget{{Team: &v1.ScopeWholeTypeSelector{Name: "*"}}},
				Exclude: []v1.ScopeTarget{{Team: &v1.ScopeWholeTypeSelector{Name: "*"}}},
			}, "only playbook can be excluded"},
			{"an exclusion with two types", v1.ScopeSpec{
				Targets: allPlaybooks,
				Exclude: []v1.ScopeTarget{{Playbook: &v1.ScopePlaybookRef{Name: "a"}, View: &v1.ScopeViewRef{Name: "a"}}},
			}, "exactly one resource type"},
			{"an empty exclusion", v1.ScopeSpec{Targets: allPlaybooks, Exclude: []v1.ScopeTarget{{Playbook: &v1.ScopePlaybookRef{}}}}, "empty selector"},
			{"an exclusion with a suffix pattern", v1.ScopeSpec{Targets: allPlaybooks, Exclude: []v1.ScopeTarget{playbook("", "*-production")}}, "other patterns aren't supported"},
			{"more than 10 exclusions", v1.ScopeSpec{Targets: allPlaybooks, Exclude: lo.Times(11, func(int) v1.ScopeTarget { return playbook("", "a") })}, "at most 10 exclusions"},
			{"exclusions and no targets", v1.ScopeSpec{Exclude: []v1.ScopeTarget{playbook("", "a")}}, "at least one target"},
		} {
			ginkgo.It("rejects "+tt.name, func() {
				Expect(ValidateScopeSpec(tt.spec)).To(MatchError(ContainSubstring(tt.message)))
			})
		}
	})

	ginkgo.It("builds membership from the targets and the exclusions", func() {
		Expect(MembershipTargets(everyPlaybookExcept)).To(Equal([]membership.Target{
			{Type: policy.ResourcePlaybook, Selector: types.ResourceSelector{Name: "*"}},
			{Type: policy.ResourcePlaybook, Selector: types.ResourceSelector{Namespace: "default", Name: "delete-production"}, Exclude: true},
			{Type: policy.ResourcePlaybook, Selector: types.ResourceSelector{Namespace: "break-glass"}, Exclude: true},
		}))
		Expect(membership.WholeTypes(MembershipTargets(everyPlaybookExcept))).To(BeEmpty())
	})

	ginkgo.Describe("a whole-type target with an exclusion of its type", func() {
		excluding := NewScopeSelection("00000000-0000-4000-8000-000000000010", "every-playbook-except", everyPlaybookExcept)
		whole := NewScopeSelection("00000000-0000-4000-8000-000000000011", "all-playbooks", v1.ScopeSpec{Targets: allPlaybooks})

		ginkgo.It("doesn't select the whole type", func() {
			Expect(excluding.SelectsEntirely(policy.ResourcePlaybook)).To(BeFalse())
			Expect(whole.SelectsEntirely(policy.ResourcePlaybook)).To(BeTrue())
		})

		for _, action := range []string{policy.ActionCreate, policy.ActionUpdate, policy.ActionDelete} {
			ginkgo.It("can't back "+action+", which is only checked on whole types", func() {
				contract, _ := ContractFor(action)
				Expect(contract.validateInput("resource", whole)).To(Succeed())
				Expect(contract.validateInput("resource", excluding)).To(MatchError(ContainSubstring("can't take a scope that excludes playbook")))
			})
		}

		ginkgo.It("backs playbook:run, which is checked per request", func() {
			run, _ := ContractFor(policy.ActionPlaybookRun)
			Expect(run.validateInput("resource", excluding)).To(Succeed())
		})

		ginkgo.It("compiles read to a row filter, without the whole-type object", func() {
			read, _ := ContractFor(policy.ActionRead)
			Expect(read.validateInput("resource", excluding)).To(Succeed())

			rule := CompiledRule{ID: "binding:1/read", Name: "read", Contract: read, Resource: []ScopeSelection{excluding}}
			policies, err := compiledRuleToCasbinRules("binding:default/ops", rule)
			Expect(err).ToNot(HaveOccurred())
			Expect(policies).To(HaveLen(1))
			Expect(policies[0][2]).To(Equal("*"), "only the condition over the playbook's scopes")

			Expect(rule.ReadGrants()).To(HaveKey(policy.ResourcePlaybook))
		})

		ginkgo.It("keeps its exclusions when it narrows a rule as a constraint", func() {
			read, _ := ContractFor(policy.ActionRead)
			narrowed, err := narrowInput(read, "resource", whole, excluding)
			Expect(err).ToNot(HaveOccurred())
			Expect(narrowed.Exclusions).To(Equal(excluding.Exclusions))

			rule := CompiledRule{Contract: read, Resource: []ScopeSelection{whole, narrowed}}
			Expect(rule.SelectsEntirely(policy.ResourcePlaybook)).To(BeFalse())
			Expect(rule.wholeTypeObjects()).To(BeEmpty())

			update, _ := ContractFor(policy.ActionUpdate)
			_, err = narrowInput(update, "resource", whole, excluding)
			Expect(err).To(HaveOccurred(), "a constraint that excludes can't narrow a rule checked on whole types")
		})
	})
})
