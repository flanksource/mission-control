package rbac

import (
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("dbTables", func() {
	ginkgo.It("only lists tables duty's /db map gives a grantable object", func() {
		for table := range dbTables {
			object := rbac.GetObjectByTable(table)
			Expect(object).ToNot(BeEmpty(), table)
			Expect(object).ToNot(Equal(policy.ObjectAuthConfidential), table)
		}
	})

	for _, table := range []string{
		"view_fake/../access_tokens",
		"view_fake/../../db/casbin_rule",
		"configs/../casbin_rule",
		"./casbin_rule",
		"view_fake/x",
	} {
		ginkgo.It("doesn't serve "+table, func() {
			_, served := servedTable(table)
			Expect(served).To(BeFalse())
		})
	}

	for _, table := range []string{"configs", "rpc/related_configs", "view_default_pods"} {
		ginkgo.It("serves "+table, func() {
			_, served := servedTable(table)
			Expect(served).To(BeTrue())
		})
	}
})
