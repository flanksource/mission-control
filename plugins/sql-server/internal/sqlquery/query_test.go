package sqlquery

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSqlQuery(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "SqlQuery")
}

var _ = ginkgo.Describe("formatGUID", func() {
	// Cross-checked against mssql.UniqueIdentifier.String(): the wire layout
	// for "01234567-89AB-CDEF-0123-456789ABCDEF" stores the first three groups
	// little-endian, e.g. byte 0 of "01234567" is 0x67 not 0x01.
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{
			name: "all zeroes",
			in:   make([]byte, 16),
			want: "00000000-0000-0000-0000-000000000000",
		},
		{
			name: "ordered example",
			in: []byte{
				0x67, 0x45, 0x23, 0x01, // time-low (reversed)
				0xAB, 0x89, // time-mid (reversed)
				0xEF, 0xCD, // time-hi (reversed)
				0x01, 0x23, // clock-seq (big-endian)
				0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, // node (big-endian)
			},
			want: "01234567-89ab-cdef-0123-456789abcdef",
		},
	}
	for _, tc := range cases {
		ginkgo.It(tc.name, func() {
			Expect(formatGUID(tc.in)).To(Equal(tc.want))
		})
	}
})

var _ = ginkgo.Describe("normalizeValue", func() {
	ginkgo.It("formats UNIQUEIDENTIFIER bytes as a GUID", func() {
		raw := []byte{0x67, 0x45, 0x23, 0x01, 0xAB, 0x89, 0xEF, 0xCD,
			0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
		Expect(normalizeValue(raw, "UNIQUEIDENTIFIER")).
			To(Equal("01234567-89ab-cdef-0123-456789abcdef"))
	})
	ginkgo.It("treats other []byte payloads as text", func() {
		Expect(normalizeValue([]byte("hello"), "VARCHAR")).To(Equal("hello"))
	})
	ginkgo.It("falls through unknown types unchanged", func() {
		Expect(normalizeValue(int64(42), "INT")).To(Equal(int64(42)))
	})
	ginkgo.It("does not GUID-format 16-byte payloads when the column is not UNIQUEIDENTIFIER", func() {
		raw := make([]byte, 16)
		// 16 NUL bytes as a binary column should stay as a string of NULs.
		Expect(normalizeValue(raw, "VARBINARY")).To(Equal(string(raw)))
	})
})
