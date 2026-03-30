package oidc

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/flanksource/incident-commander/auth/oidcclient"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("OIDC Browser Login Flow", ginkgo.Label("slow"), ginkgo.Ordered, func() {
	var tokens *oidcclient.Tokens
	var endpoints *oidcclient.Discovery

	ginkgo.It("completes full OIDC authorization code flow via browser", func() {
		verifier, challenge, err := oidcclient.GeneratePKCE()
		Expect(err).ToNot(HaveOccurred())

		state, err := oidcclient.RandomBase64(16)
		Expect(err).ToNot(HaveOccurred())
		nonce, err := oidcclient.RandomBase64(16)
		Expect(err).ToNot(HaveOccurred())

		endpoints, err = oidcclient.Discover(serverURL + "/.well-known/openid-configuration")
		Expect(err).ToNot(HaveOccurred())
		Expect(endpoints.AuthorizationEndpoint).ToNot(BeEmpty())
		Expect(endpoints.TokenEndpoint).ToNot(BeEmpty())

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).ToNot(HaveOccurred())
		callbackPort := listener.Addr().(*net.TCPAddr).Port
		redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", callbackPort)

		codeCh := make(chan string, 1)
		errCh := make(chan error, 1)
		callbackServer := &http.Server{}
		callbackServer.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" {
				http.NotFound(w, r)
				return
			}
			q := r.URL.Query()
			if s := q.Get("state"); s != state {
				errCh <- fmt.Errorf("state mismatch: got %s", s)
				http.Error(w, "state mismatch", http.StatusBadRequest)
				return
			}
			if e := q.Get("error"); e != "" {
				errCh <- fmt.Errorf("auth error: %s: %s", e, q.Get("error_description"))
				http.Error(w, e, http.StatusBadRequest)
				return
			}
			code := q.Get("code")
			if code == "" {
				errCh <- fmt.Errorf("missing code")
				http.Error(w, "missing code", http.StatusBadRequest)
				return
			}
			fmt.Fprint(w, "Login successful")
			codeCh <- code
		})
		go func() {
			if err := callbackServer.Serve(listener); err != nil && err != http.ErrServerClosed {
				errCh <- err
			}
		}()
		defer func() { _ = callbackServer.Shutdown(context.Background()) }()

		authURL := fmt.Sprintf("%s?client_id=mc-cli&response_type=code&scope=%s&redirect_uri=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256",
			endpoints.AuthorizationEndpoint,
			url.QueryEscape("openid profile email offline_access"),
			url.QueryEscape(redirectURI),
			url.QueryEscape(state),
			url.QueryEscape(nonce),
			url.QueryEscape(challenge),
		)

		err = chromedp.Run(chromectx,
			chromedp.Navigate(authURL),
			chromedp.WaitVisible(`input[name="username"]`, chromedp.ByQuery),
			chromedp.SendKeys(`input[name="username"]`, "admin", chromedp.ByQuery),
			chromedp.SendKeys(`input[name="password"]`, "admin", chromedp.ByQuery),
			chromedp.Click(`button[type="submit"]`, chromedp.ByQuery),
		)
		Expect(err).ToNot(HaveOccurred())

		var code string
		select {
		case code = <-codeCh:
		case err := <-errCh:
			ginkgo.Fail(fmt.Sprintf("callback error: %v", err))
		case <-time.After(30 * time.Second):
			ginkgo.Fail("timed out waiting for callback")
		}
		Expect(code).ToNot(BeEmpty())

		tokens, err = oidcclient.ExchangeCode(endpoints.TokenEndpoint, code, redirectURI, verifier)
		Expect(err).ToNot(HaveOccurred())
		Expect(tokens.AccessToken).ToNot(BeEmpty())
		Expect(tokens.IDToken).ToNot(BeEmpty())
		Expect(tokens.RefreshToken).ToNot(BeEmpty())

		Expect(oidcclient.ValidateNonce(tokens.IDToken, nonce)).To(Succeed())

		req, err := http.NewRequest("GET", serverURL+"/userinfo", nil)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)

		resp, err := http.DefaultClient.Do(req)
		Expect(err).ToNot(HaveOccurred())
		resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})

	ginkgo.It("refreshes tokens using the refresh token", func() {
		Expect(tokens).ToNot(BeNil(), "login test must run first")
		Expect(endpoints).ToNot(BeNil())

		originalAccess := tokens.AccessToken
		originalRefresh := tokens.RefreshToken

		refreshed, err := oidcclient.RefreshToken(endpoints.TokenEndpoint, originalRefresh)
		Expect(err).ToNot(HaveOccurred())
		Expect(refreshed.AccessToken).ToNot(BeEmpty())
		Expect(refreshed.RefreshToken).ToNot(BeEmpty())
		Expect(refreshed.AccessToken).ToNot(Equal(originalAccess))

		// Verify new access token works
		req, err := http.NewRequest("GET", serverURL+"/userinfo", nil)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set("Authorization", "Bearer "+refreshed.AccessToken)

		resp, err := http.DefaultClient.Do(req)
		Expect(err).ToNot(HaveOccurred())
		resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})
})
