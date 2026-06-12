package clientcmd

import (
	"io"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = ginkgo.Describe("auth login --token", func() {
	ginkgo.BeforeEach(func() {
		dir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", dir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", dir)
	})

	ginkgo.AfterEach(func() {
		loginServer = ""
		loginToken = ""
	})

	ginkgo.It("stores the provided token without starting the OIDC flow", func() {
		loginServer = "http://mc.example.com"
		loginToken = "my-access-token"

		cmd := &cobra.Command{}
		cmd.SetOut(io.Discard)
		Expect(runAuthLogin(cmd, nil)).To(Succeed())

		stored, err := LoadStoredTokens("http://mc.example.com")
		Expect(err).ToNot(HaveOccurred())
		Expect(stored.AccessToken).To(Equal("my-access-token"))
	})

	ginkgo.It("trims a trailing slash from the server before storing", func() {
		loginServer = "http://mc.example.com/"
		loginToken = "tok2"

		cmd := &cobra.Command{}
		cmd.SetOut(io.Discard)
		Expect(runAuthLogin(cmd, nil)).To(Succeed())

		stored, err := LoadStoredTokens("http://mc.example.com")
		Expect(err).ToNot(HaveOccurred())
		Expect(stored.AccessToken).To(Equal("tok2"))
	})
})
