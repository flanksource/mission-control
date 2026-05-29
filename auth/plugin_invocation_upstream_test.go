package auth

import (
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("plugin invocation tokens", func() {
	ginkgo.It("preserves subject and roles", func() {
		pluginID := uuid.New()

		token, err := MintPluginInvocationToken("user-1", pluginID, 0, "admin")
		Expect(err).NotTo(HaveOccurred())

		claims, err := VerifyPluginInvocationToken(token, pluginID)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims.Subject).To(Equal("user-1"))
		Expect(claims.Roles).To(Equal([]string{"admin"}))
	})
})
