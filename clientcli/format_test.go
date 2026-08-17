package clientcli

import (
	"strings"

	"github.com/flanksource/incident-commander/clientcli/api"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type prettyFixture struct {
	Raw string
}

func (prettyFixture) Pretty() api.Text {
	return Text("custom summary")
}

type recordFixture struct {
	Name string `json:"name"`
}

var _ = ginkgo.Describe("lightweight formatting", func() {
	ginkgo.It("uses the Pretty contract for detail output", func() {
		output, err := Format(prettyFixture{Raw: "internal"}, FormatOptions{NoColor: true})

		Expect(err).ToNot(HaveOccurred())
		Expect(output).To(Equal("custom summary"))
		Expect(output).ToNot(ContainSubstring("internal"))
	})

	ginkgo.It("formats empty typed slices as CSV", func() {
		output, err := Format([]recordFixture{}, FormatOptions{CSV: true})

		Expect(err).ToNot(HaveOccurred())
		Expect(output).To(Equal("Name"))
	})

	ginkgo.It("handles nil pointer rows", func() {
		output, err := Format([]*recordFixture{{Name: "first"}, nil}, FormatOptions{CSV: true})

		Expect(err).ToNot(HaveOccurred())
		Expect(output).To(ContainSubstring("first"))
	})

	ginkgo.It("returns standalone HTML", func() {
		output, err := Format([]recordFixture{{Name: "first"}}, FormatOptions{HTML: true})

		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToLower(output)).To(HavePrefix("<!doctype html>"))
		Expect(output).To(ContainSubstring("<table>"))
	})

	ginkgo.It("reports unsupported tree output", func() {
		_, err := Format([]recordFixture{}, FormatOptions{Tree: true})

		Expect(err).To(MatchError(ContainSubstring("--tree is not available")))
	})
})
