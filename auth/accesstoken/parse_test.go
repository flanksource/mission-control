package accesstoken

import (
	"testing"

	"github.com/flanksource/incident-commander/auth/signing"
	"github.com/onsi/gomega"
)

func TestParse(t *testing.T) {
	g := gomega.NewWithT(t)

	_, _, err := signing.Initialize("/tmp/undefined")
	g.Expect(err).To(gomega.BeNil())

	token, err := Generate()
	g.Expect(err).To(gomega.BeNil())

	v2, err := Parse(string(token.V2()))
	g.Expect(err).To(gomega.BeNil())
	g.Expect(token).To(gomega.Equal(v2))
}
