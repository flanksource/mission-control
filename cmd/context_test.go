package cmd

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/flanksource/incident-commander/auth/oidcclient"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = ginkgo.Describe("context token resolution", func() {
	var oldOIDCLogin func(*cobra.Command, string, io.Writer) (*oidcclient.Tokens, string, error)

	ginkgo.BeforeEach(func() {
		oldOIDCLogin = oidcLogin
		ginkgo.GinkgoT().Setenv("HOME", ginkgo.GinkgoT().TempDir())
	})

	ginkgo.AfterEach(func() {
		oidcLogin = oldOIDCLogin
	})

	ginkgo.It("reuses a stored access token before starting OIDC login", func() {
		server := "http://mission-control.local"
		_, err := storeTokens(server, &oidcclient.Tokens{
			AccessToken: "stored-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		})
		Expect(err).ToNot(HaveOccurred())

		oidcLogin = func(*cobra.Command, string, io.Writer) (*oidcclient.Tokens, string, error) {
			return nil, "", fmt.Errorf("unexpected login")
		}

		ctx := &MCContext{Name: "local", Server: server}
		Expect(ensureContextToken(&cobra.Command{}, ctx, io.Discard)).To(Succeed())
		Expect(ctx.Token).To(Equal("stored-token"))
	})

	ginkgo.It("starts OIDC login when no usable token is available", func() {
		var stderr bytes.Buffer
		oidcLogin = func(_ *cobra.Command, server string, status io.Writer) (*oidcclient.Tokens, string, error) {
			Expect(server).To(Equal("http://mission-control.local"))
			fmt.Fprint(status, "login started")
			return &oidcclient.Tokens{AccessToken: "oauth-token"}, "", nil
		}

		ctx := &MCContext{Name: "local", Server: "http://mission-control.local"}
		Expect(ensureContextToken(&cobra.Command{}, ctx, &stderr)).To(Succeed())
		Expect(ctx.Token).To(Equal("oauth-token"))
		Expect(stderr.String()).To(ContainSubstring("starting OIDC login"))
		Expect(stderr.String()).To(ContainSubstring("login started"))
	})
})
