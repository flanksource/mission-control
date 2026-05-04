package main

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSQLServerPlugin(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "SQLServerPlugin")
}

var _ = ginkgo.Describe("withDefaultDatabase", func() {
	cases := []struct {
		name     string
		connType string
		input    string
		database string
		want     string
	}{
		{
			name:     "sqlserver URL with no params",
			connType: "sql_server",
			input:    "sqlserver://user:pw@host:1433",
			database: "MyDb",
			want:     "sqlserver://user:pw@host:1433?database=MyDb",
		},
		{
			name:     "sqlserver URL overwrites existing database",
			connType: "sql_server",
			input:    "sqlserver://host?database=other&app=foo",
			database: "MyDb",
			want:     "sqlserver://host?app=foo&database=MyDb",
		},
		{
			name:     "postgres URL uses dbname",
			connType: "postgres",
			input:    "postgres://user@host:5432/postgres?sslmode=disable",
			database: "myapp",
			want:     "postgres://user@host:5432/postgres?dbname=myapp&sslmode=disable",
		},
		{
			name:     "mysql URL uses database",
			connType: "mysql",
			input:    "mysql://user:pw@host:3306/?parseTime=true",
			database: "shop",
			want:     "mysql://user:pw@host:3306/?database=shop&parseTime=true",
		},
		{
			name:     "database name with special characters is URL-encoded",
			connType: "sql_server",
			input:    "sqlserver://host",
			database: "my db",
			want:     "sqlserver://host?database=my+db",
		},
	}
	for _, tc := range cases {
		ginkgo.It(tc.name, func() {
			got, err := withDefaultDatabase(tc.connType, tc.input, tc.database)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(tc.want))
		})
	}

	ginkgo.It("rejects DSN-style strings that aren't URLs", func() {
		_, err := withDefaultDatabase("sql_server", "server=host;user id=sa;password=x", "MyDb")
		Expect(err).To(HaveOccurred())
	})
})
