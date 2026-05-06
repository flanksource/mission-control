package main

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPostgresPlugin(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Postgres Plugin")
}

var _ = ginkgo.Describe("withDefaultDatabase", func() {
	for _, tt := range []struct {
		name     string
		input    string
		database string
		want     string
	}{
		{
			name:     "replaces URL path database",
			input:    "postgres://user:pw@host:5432/postgres?sslmode=disable",
			database: "appdb",
			want:     "postgres://user:pw@host:5432/appdb?sslmode=disable",
		},
		{
			name:     "removes dbname query parameter",
			input:    "postgres://user@host/postgres?application_name=mc&dbname=old",
			database: "newdb",
			want:     "postgres://user@host/newdb?application_name=mc",
		},
		{
			name:     "escapes URL path database",
			input:    "postgres://host/postgres",
			database: "my db",
			want:     "postgres://host/my%20db",
		},
		{
			name:     "replaces keyword DSN dbname",
			input:    "host=localhost user=postgres dbname=postgres sslmode=disable",
			database: "appdb",
			want:     "host=localhost user=postgres dbname=appdb sslmode=disable",
		},
		{
			name:     "appends keyword DSN dbname",
			input:    "host=localhost user=postgres",
			database: "appdb",
			want:     "host=localhost user=postgres dbname=appdb",
		},
	} {
		ginkgo.It(tt.name, func() {
			got, err := withDefaultDatabase(tt.input, tt.database)
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(tt.want))
		})
	}
})
