// ABOUTME: Tests for scope impersonation logic that allows admins to simulate
// ABOUTME: RLS restrictions via the X-Flanksource-Scope header.
package auth

import (
	"github.com/flanksource/duty/rls"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	scopeA = "00000000-0000-4000-8000-00000000000a"
	scopeB = "00000000-0000-4000-8000-00000000000b"
	scopeX = "00000000-0000-4000-8000-0000000000ff"
)

func grantsOf(grants ...rls.Grant) *rls.Grants {
	g := rls.NoRows()
	for _, grant := range grants {
		g.Add(grant)
	}
	return g
}

var _ = Describe("parseImpersonatedScopes", func() {
	It("reads a JSON array of scope ids", func() {
		ids, err := parseImpersonatedScopes(`["` + scopeB + `", "` + scopeA + `", "` + scopeB + `"]`)
		Expect(err).ToNot(HaveOccurred())
		Expect(ids).To(Equal([]string{scopeA, scopeB}))
	})

	for _, header := range []string{`{"config":[{"tags":{"a":"b"}}]}`, `["staging"]`, `scope`} {
		It("rejects "+header, func() {
			_, err := parseImpersonatedScopes(header)
			Expect(err).To(HaveOccurred())
		})
	}
})

var _ = Describe("applyScopeLimits", func() {
	It("returns the real payload without the header", func() {
		real := &rls.Payload{Disable: true}
		Expect(applyScopeLimits(real, nil)).To(BeIdenticalTo(real))
	})

	It("grants only the header's Scopes to a subject whose listings aren't filtered", func() {
		got := applyScopeLimits(&rls.Payload{Disable: true}, [][]string{{scopeX}})
		Expect(got.Disable).To(BeFalse())
		for _, kind := range rls.GrantTypes {
			Expect(got.GrantsFor(kind).Any).To(Equal([]rls.Grant{{Impersonated: []string{scopeX}}}), kind)
		}
		Expect(got.Scopes).To(Equal([]string{scopeX}))
		Expect(got.View).To(Equal([]rls.Scope{{ID: "*"}}))
	})

	It("lists the rows in any of the header's Scopes", func() {
		got := applyScopeLimits(&rls.Payload{Disable: true}, [][]string{{scopeX, scopeA}})
		Expect(got.Config.Any).To(ConsistOf(rls.Grant{Impersonated: []string{scopeX}}, rls.Grant{Impersonated: []string{scopeA}}))

		real := &rls.Payload{Config: grantsOf(rls.Grant{Scope: scopeB})}
		got = applyScopeLimits(real, [][]string{{scopeX, scopeA}})
		Expect(got.Config.Any).To(ConsistOf(
			rls.Grant{Scope: scopeB, Impersonated: []string{scopeX}},
			rls.Grant{Scope: scopeB, Impersonated: []string{scopeA}},
		))
	})

	It("limits every grant to the header's Scopes, so it can only narrow", func() {
		real := &rls.Payload{
			Config:    grantsOf(rls.Grant{Scope: scopeA}, rls.Grant{Scope: scopeB}),
			Component: rls.AllRows(),
			Check:     rls.NoRows(),
			Scopes:    []string{scopeA, scopeX},
		}
		got := applyScopeLimits(real, [][]string{{scopeX}})

		Expect(got.Config.Any).To(Equal([]rls.Grant{
			{Scope: scopeA, Impersonated: []string{scopeX}},
			{Scope: scopeB, Impersonated: []string{scopeX}},
		}))
		Expect(got.Component.Any).To(Equal([]rls.Grant{{Impersonated: []string{scopeX}}}))
		Expect(got.Check.IsEmpty()).To(BeTrue())
		Expect(got.Playbook).To(BeNil())
		Expect(got.Scopes).To(Equal([]string{scopeX}))

		Expect(real.Config.Any).To(Equal([]rls.Grant{{Scope: scopeA}, {Scope: scopeB}}), "the cached real payload isn't changed")
	})

	It("lists nothing for a header naming no Scope", func() {
		got := applyScopeLimits(&rls.Payload{Disable: true}, [][]string{{}})
		Expect(got.Disable).To(BeFalse())
		Expect(got.Config).To(BeNil())
	})
})
