package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("identityAccess principal types", func() {
	projection := func(principalTypes []string, userTypes []ProjectionUserTypeRule) Projection {
		return Projection{
			APIVersion: projectionAPIVersion,
			Kind:       projectionKind,
			Metadata:   ProjectionMetadata{Name: "identity-access"},
			Spec: ProjectionSpec{Source: ProjectionSource{Query: ProjectionQuery{
				IdentityAccess: &ProjectionIdentityAccessQuery{
					Limit: 100, PrincipalTypes: principalTypes, UserTypes: userTypes,
				},
			}}},
		}
	}

	ginkgo.It("requires at least one principal type", func() {
		Expect(projection(nil, defaultIdentityTypeRules()).validate()).To(MatchError(ContainSubstring("principalTypes must contain at least one")))
	})

	ginkgo.It("allows groups without user classification rules", func() {
		Expect(projection([]string{"groups"}, nil).validate()).To(Succeed())
	})

	ginkgo.It("requires user classification rules when users are selected", func() {
		Expect(projection([]string{"users", "groups"}, nil).validate()).To(MatchError(ContainSubstring("userTypes must contain at least one")))
	})

	ginkgo.It("rejects unsupported principal types", func() {
		Expect(projection([]string{"roles"}, nil).validate()).To(MatchError(ContainSubstring("principalTypes[0] must be users or groups")))
	})
})
