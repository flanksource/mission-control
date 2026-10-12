package v1

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("RoleBindingSpec.Validate", func() {
	for _, tt := range []struct {
		name     string
		subjects RoleBindingSubjects
		err      string
	}{
		{name: "agents by name", subjects: RoleBindingSubjects{Agents: []string{"eu-cluster", "us-cluster"}}},
		{name: "every agent", subjects: RoleBindingSubjects{Agents: []string{"*"}}},
		{name: "every agent alongside a name", subjects: RoleBindingSubjects{Agents: []string{"*", "eu-cluster"}}, err: `agent "*" must be the only agent subject`},
		{name: "an empty agent", subjects: RoleBindingSubjects{Agents: []string{""}}, err: "agent name is required"},
		{name: "an agent prefix", subjects: RoleBindingSubjects{Agents: []string{"eu-*"}}, err: `agent "eu-*" must be an exact name`},
		{name: "an agent list", subjects: RoleBindingSubjects{Agents: []string{"eu,us"}}, err: `agent "eu,us" must be an exact name`},
		{name: "a plugin by namespace and name", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Namespace: "mission-control", Name: "kubernetes-logs"}}}},
		{name: "plugins by namespace", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Namespace: "mission-control"}}}},
		{name: "every plugin", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Name: "*"}}}},
		{name: "an empty plugin", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{}}}, err: "plugins subject 0: namespace or name is required"},
		{name: "a plugin in every namespace", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Namespace: "*", Name: "kubernetes-logs"}}}, err: `namespace "*" must be an exact value`},
		{name: "a plugin name prefix", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Name: "kubernetes-*"}}}, err: `name "kubernetes-*" must be an exact value`},
		{name: "a plugin name list", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Name: "a,b"}}}, err: `name "a,b" must be an exact value`},
		{name: "a plugin name exclusion", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Name: "!a"}}}, err: `name "!a" must be an exact value`},
	} {
		ginkgo.It(tt.name, func() {
			err := RoleBindingSpec{Role: "connections", Subjects: tt.subjects}.Validate()
			if tt.err == "" {
				Expect(err).ToNot(HaveOccurred())
			} else {
				Expect(err).To(MatchError(ContainSubstring(tt.err)))
			}
		})
	}

	ginkgo.It("counts agents and plugins as subjects", func() {
		Expect(RoleBindingSubjects{Agents: []string{"eu-cluster"}}.Empty()).To(BeFalse())
		Expect(RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Name: "*"}}}.Empty()).To(BeFalse())
		Expect(RoleBindingSpec{Role: "connections"}.Validate()).To(MatchError(ContainSubstring("at least one subject is required")))
	})
})
