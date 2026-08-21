package clientcmd

import (
	"strings"

	"github.com/flanksource/incident-commander/clientapi"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("connection session rendering", func() {
	ginkgo.It("sorts cookie domain summaries", func() {
		text := prettyCookies(clientapi.Cookies{
			{Name: "b", Domain: "z.example"},
			{Name: "a", Domain: "a.example"},
			{Name: "a2", Domain: "a.example"},
		}, false).String()

		a := strings.Index(text, "a.example(2)")
		z := strings.Index(text, "z.example(1)")
		Expect(a).To(BeNumerically(">=", 0))
		Expect(z).To(BeNumerically(">", a))
	})
})
