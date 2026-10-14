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
		{name: "a plugin by namespace and name", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Namespace: "mission-control", Name: "kubernetes-logs"}}}},
		{name: "plugins by namespace", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Namespace: "mission-control"}}}},
		{name: "every plugin", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Name: "*"}}}},
		{name: "an empty plugin", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{}}}, err: "plugins subject 0: namespace or name is required"},
		{name: "a plugin in every namespace", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Namespace: "*", Name: "kubernetes-logs"}}}, err: `namespace "*" must be an exact value`},
		{name: "a plugin name prefix", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Name: "kubernetes-*"}}}, err: `name "kubernetes-*" must be an exact value`},
		{name: "a plugin name with a comma, which is literal", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Name: "a,b"}}}},
		{name: "a plugin name starting with !, which is literal", subjects: RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Name: "!a"}}}},
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

	ginkgo.It("counts plugins as subjects", func() {
		Expect(RoleBindingSubjects{Plugins: []PermissionGroupSelector{{Name: "*"}}}.Empty()).To(BeFalse())
		Expect(RoleBindingSpec{Role: "connections"}.Validate()).To(MatchError(ContainSubstring("at least one subject is required")))
	})
})

var _ = ginkgo.Describe("ParseReference", func() {
	for _, tt := range []struct {
		ref       string
		namespace string
		name      string
		err       string
	}{
		{ref: "admin", namespace: "platform", name: "admin"},
		{ref: "mission-control-builtin/admin", namespace: "mission-control-builtin", name: "admin"},
		{ref: "platform/admin", namespace: "platform", name: "admin"},
		{ref: "team.a/admin", err: `namespace "team.a" is invalid`},
		{ref: "/admin", err: `namespace "" is invalid`},
		{ref: "platform/", err: `name "" is invalid`},
		{ref: "a/b/c", err: `name "b/c" is invalid`},
		{ref: "Admin", err: `name "Admin" is invalid`},
	} {
		ginkgo.It(tt.ref, func() {
			namespace, name, err := ParseReference("platform", tt.ref)
			if tt.err != "" {
				Expect(err).To(MatchError(ContainSubstring(tt.err)))
				return
			}
			Expect(err).ToNot(HaveOccurred())
			Expect([]string{namespace, name}).To(Equal([]string{tt.namespace, tt.name}))
		})
	}

	ginkgo.It("rejects a malformed role or constraint reference in a RoleBinding", func() {
		subjects := RoleBindingSubjects{PermissionGroupSubjects: PermissionGroupSubjects{People: []string{"*"}}}
		Expect(RoleBindingSpec{Role: "a/b/c", Subjects: subjects}.Validate()).To(MatchError(ContainSubstring("role")))
		Expect(RoleBindingSpec{Role: "admin", Subjects: subjects, Constraint: &RoleBindingConstraint{
			Resource: &ScopeReference{ScopeRef: "/configs"},
		}}.Validate()).To(MatchError(ContainSubstring("constraint: resource.scopeRef")))
	})
})
