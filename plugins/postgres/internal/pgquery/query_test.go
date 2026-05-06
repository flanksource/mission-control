package pgquery

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPGQuery(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "PGQuery")
}

var _ = ginkgo.Describe("rowReturning", func() {
	for _, tt := range []struct {
		name      string
		statement string
		want      bool
	}{
		{name: "select", statement: "select 1", want: true},
		{name: "show", statement: "SHOW server_version", want: true},
		{name: "with", statement: "WITH x AS (SELECT 1) SELECT * FROM x", want: true},
		{name: "insert returning", statement: "INSERT INTO t VALUES (1) RETURNING id", want: true},
		{name: "update no returning", statement: "UPDATE t SET x = 1", want: false},
	} {
		ginkgo.It(tt.name, func() {
			Expect(rowReturning(tt.statement)).To(Equal(tt.want))
		})
	}
})
