// ABOUTME: Tests for scope impersonation logic that allows admins to simulate
// ABOUTME: RLS restrictions via the X-Flanksource-Scope header.
package auth

import (
	"sort"

	"github.com/flanksource/duty/rls"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// sortScope sorts slice fields for deterministic comparison.
func sortScope(s *rls.Scope) {
	sort.Strings(s.Agents)
	sort.Strings(s.Names)
}

var _ = Describe("intersectScope", func() {
	DescribeTable("pairwise scope intersection",
		func(a, b rls.Scope, wantOK bool, want rls.Scope) {
			got, ok := intersectScope(a, b)
			Expect(ok).To(Equal(wantOK))
			if !wantOK {
				return
			}
			sortScope(&got)
			sortScope(&want)
			Expect(got).To(Equal(want))
		},
		Entry("both empty",
			rls.Scope{}, rls.Scope{},
			true, rls.Scope{}),
		Entry("matching tags merge",
			rls.Scope{Tags: map[string]string{"team": "backend"}},
			rls.Scope{Tags: map[string]string{"env": "prod"}},
			true, rls.Scope{Tags: map[string]string{"team": "backend", "env": "prod"}}),
		Entry("conflicting tags",
			rls.Scope{Tags: map[string]string{"team": "backend"}},
			rls.Scope{Tags: map[string]string{"team": "frontend"}},
			false, rls.Scope{}),
		Entry("overlapping agents",
			rls.Scope{Agents: []string{"a", "b"}},
			rls.Scope{Agents: []string{"b", "c"}},
			true, rls.Scope{Agents: []string{"b"}}),
		Entry("disjoint agents",
			rls.Scope{Agents: []string{"a"}},
			rls.Scope{Agents: []string{"b"}},
			false, rls.Scope{}),
		Entry("one side unrestricted agents",
			rls.Scope{Agents: []string{"a", "b"}},
			rls.Scope{},
			true, rls.Scope{Agents: []string{"a", "b"}}),
		Entry("overlapping names",
			rls.Scope{Names: []string{"x", "y"}},
			rls.Scope{Names: []string{"y", "z"}},
			true, rls.Scope{Names: []string{"y"}}),
		Entry("disjoint names",
			rls.Scope{Names: []string{"x"}},
			rls.Scope{Names: []string{"y"}},
			false, rls.Scope{}),
		Entry("same ID",
			rls.Scope{ID: "abc"},
			rls.Scope{ID: "abc"},
			true, rls.Scope{ID: "abc"}),
		Entry("different IDs",
			rls.Scope{ID: "abc"},
			rls.Scope{ID: "def"},
			false, rls.Scope{}),
		Entry("one ID set",
			rls.Scope{ID: "abc"},
			rls.Scope{},
			true, rls.Scope{ID: "abc"}),
		Entry("combined fields",
			rls.Scope{Tags: map[string]string{"team": "backend"}, Agents: []string{"a", "b"}},
			rls.Scope{Tags: map[string]string{"env": "prod"}, Agents: []string{"b", "c"}},
			true, rls.Scope{Tags: map[string]string{"team": "backend", "env": "prod"}, Agents: []string{"b"}}),
	)
})

var _ = Describe("intersectScopeList", func() {
	DescribeTable("scope list intersection",
		func(real, impersonated, want []rls.Scope) {
			got := intersectScopeList(real, impersonated)
			if len(want) == 0 {
				Expect(got).To(BeEmpty())
			} else {
				Expect(got).To(Equal(want))
			}
		},
		Entry("empty impersonated returns empty",
			[]rls.Scope{{ID: "a"}}, nil, nil),
		Entry("empty real returns empty",
			nil, []rls.Scope{{ID: "a"}}, nil),
		Entry("matching IDs kept",
			[]rls.Scope{{ID: "a"}, {ID: "b"}},
			[]rls.Scope{{ID: "a"}},
			[]rls.Scope{{ID: "a"}}),
		Entry("no overlap produces empty",
			[]rls.Scope{{ID: "a"}},
			[]rls.Scope{{ID: "b"}},
			nil),
		Entry("cartesian product of tag scopes",
			[]rls.Scope{{Tags: map[string]string{"team": "backend"}}},
			[]rls.Scope{{Tags: map[string]string{"env": "prod"}}},
			[]rls.Scope{{Tags: map[string]string{"team": "backend", "env": "prod"}}}),
		Entry("multiple real scopes with one impersonated",
			[]rls.Scope{
				{Tags: map[string]string{"team": "backend"}},
				{Tags: map[string]string{"team": "frontend"}},
			},
			[]rls.Scope{
				{Tags: map[string]string{"team": "backend"}},
			},
			[]rls.Scope{
				{Tags: map[string]string{"team": "backend"}},
			}),
	)
})

var _ = Describe("applyImpersonation", func() {
	DescribeTable("payload impersonation",
		func(real, impersonate, want *rls.Payload, wantErr bool) {
			got, err := applyImpersonation(real, impersonate)
			if wantErr {
				Expect(err).To(HaveOccurred())
				return
			}
			Expect(err).ToNot(HaveOccurred())
			Expect(got.Disable).To(Equal(want.Disable))
			expectScopeListEqual(got.Config, want.Config)
			expectScopeListEqual(got.Component, want.Component)
			expectScopeListEqual(got.Playbook, want.Playbook)
			expectScopeListEqual(got.Canary, want.Canary)
			expectScopeListEqual(got.View, want.View)
			expectStringSliceEqual(got.Scopes, want.Scopes)
		},
		Entry("no impersonation returns real payload",
			&rls.Payload{Disable: true}, nil,
			&rls.Payload{Disable: true}, false),
		Entry("admin can impersonate scopes",
			&rls.Payload{Disable: true},
			&rls.Payload{Config: []rls.Scope{{Tags: map[string]string{"team": "backend"}}}},
			&rls.Payload{Config: []rls.Scope{{Tags: map[string]string{"team": "backend"}}}},
			false),
		Entry("guest impersonation intersects with real payload",
			&rls.Payload{Config: []rls.Scope{{ID: "cfg-1"}, {ID: "cfg-2"}}},
			&rls.Payload{Config: []rls.Scope{{ID: "cfg-1"}, {ID: "cfg-3"}}},
			&rls.Payload{Config: []rls.Scope{{ID: "cfg-1"}}},
			false),
		Entry("guest impersonation with no overlap restricts to nothing",
			&rls.Payload{Config: []rls.Scope{{ID: "cfg-1"}}},
			&rls.Payload{Config: []rls.Scope{{ID: "cfg-99"}}},
			&rls.Payload{},
			false),
		Entry("guest impersonation intersects scopes list",
			&rls.Payload{Scopes: []string{"scope-a", "scope-b", "scope-c"}},
			&rls.Payload{Scopes: []string{"scope-b", "scope-d"}},
			&rls.Payload{Scopes: []string{"scope-b"}},
			false),
		Entry("impersonation with empty payload restricts to nothing",
			&rls.Payload{Disable: true},
			&rls.Payload{},
			&rls.Payload{},
			false),
		Entry("guest with multiple resource types",
			&rls.Payload{
				Config:    []rls.Scope{{ID: "cfg-1"}},
				Component: []rls.Scope{{Tags: map[string]string{"team": "backend"}}},
			},
			&rls.Payload{
				Config:    []rls.Scope{{ID: "cfg-1"}},
				Component: []rls.Scope{{Tags: map[string]string{"team": "frontend"}}},
			},
			&rls.Payload{Config: []rls.Scope{{ID: "cfg-1"}}},
			false),
	)
})

func expectScopeListEqual(got, want []rls.Scope) {
	if len(want) == 0 {
		Expect(got).To(BeEmpty())
	} else {
		Expect(got).To(Equal(want))
	}
}

func expectStringSliceEqual(got, want []string) {
	if len(want) == 0 {
		Expect(got).To(BeEmpty())
	} else {
		Expect(got).To(Equal(want))
	}
}
