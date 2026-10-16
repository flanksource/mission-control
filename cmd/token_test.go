package cmd

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/db"
)

var _ = ginkgo.Describe("auth token --role", func() {
	ginkgo.It("parses roles as namespace/name", func() {
		roles, err := parseTokenRoles([]string{"mission-control-builtin/admin", "default/runner"})
		Expect(err).NotTo(HaveOccurred())
		Expect(roles).To(Equal(db.AccessTokenRoles{
			{Namespace: "mission-control-builtin", Name: "admin"},
			{Namespace: "default", Name: "runner"},
		}))
	})

	ginkgo.It("requires at least one role", func() {
		_, err := parseTokenRoles(nil)
		Expect(err).To(MatchError(ContainSubstring("at least one --role")))
	})

	for _, value := range []string{"admin", "/admin", "default/", "a/b/c"} {
		ginkgo.It("rejects "+value, func() {
			_, err := parseTokenRoles([]string{value})
			Expect(err).To(MatchError(ContainSubstring("must be namespace/name")))
		})
	}
})
