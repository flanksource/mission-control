package host

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("connectionRefFromScraperSpec", func() {
	cases := []struct {
		name    string
		spec    string
		want    string
		wantErr string
	}{
		{
			name: "first sql entry's connection ref is returned",
			spec: `{"sql":[{"connection":"connection://default/db-a","query":"select 1"}]}`,
			want: "connection://default/db-a",
		},
		{
			name: "skips empty entries and returns the first non-empty connection",
			spec: `{"sql":[{"query":"select 1"},{"connection":"connection://default/db-b"}]}`,
			want: "connection://default/db-b",
		},
		{
			name: "uuid form is returned verbatim",
			spec: `{"sql":[{"connection":"de349d14-9e24-c79f-e817-6f05caa211f7"}]}`,
			want: "de349d14-9e24-c79f-e817-6f05caa211f7",
		},
		{
			name:    "missing sql block",
			spec:    `{"kubernetes":[{"selector":"namespace=default"}]}`,
			wantErr: "no sql.connection set",
		},
		{
			name:    "empty sql block",
			spec:    `{"sql":[]}`,
			wantErr: "no sql.connection set",
		},
		{
			name:    "sql entry without a connection ref",
			spec:    `{"sql":[{"query":"select 1"}]}`,
			wantErr: "no sql.connection set",
		},
		{
			name:    "malformed json surfaces decode error",
			spec:    `{"sql":[`,
			wantErr: "decode spec",
		},
	}

	for _, tc := range cases {
		ginkgo.It(tc.name, func() {
			got, err := connectionRefFromScraperSpec(tc.spec)
			if tc.wantErr != "" {
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(tc.wantErr))
				Expect(got).To(BeEmpty())
				return
			}
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(tc.want))
		})
	}
})
