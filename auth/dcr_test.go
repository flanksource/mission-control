package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/flanksource/incident-commander/auth/oidc"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Regression coverage for https://github.com/flanksource/mission-control/issues/2932
// — the OAuth surface Claude Desktop and Codex CLI drive when connecting to /mcp.
var _ = ginkgo.Describe("OAuth dynamic client registration", func() {
	var e *echo.Echo

	ginkgo.BeforeEach(func() {
		e = newEchoInstance(DefaultContext)
		Expect(oidc.MountRoutes(e, DefaultContext, "http://localhost:8080", &mockChecker{valid: true}, nil, mockLookup)).To(Succeed())
	})

	serve := func(req *http.Request) *httptest.ResponseRecorder {
		req = req.WithContext(DefaultContext.Wrap(req.Context()))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	registerClient := func(body string) map[string]any {
		req := httptest.NewRequest(http.MethodPost, oidc.RegistrationEndpoint, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := serve(req)

		Expect(rec.Code).To(Equal(http.StatusCreated))
		var resp map[string]any
		Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
		return resp
	}

	ginkgo.Describe("discovery", func() {
		for _, path := range []string{
			"/.well-known/openid-configuration",
			"/.well-known/oauth-authorization-server",
		} {
			ginkgo.It("advertises registration and S256 at "+path, func() {
				rec := serve(httptest.NewRequest(http.MethodGet, path, nil))
				Expect(rec.Code).To(Equal(http.StatusOK))

				var doc map[string]any
				Expect(json.Unmarshal(rec.Body.Bytes(), &doc)).To(Succeed())

				Expect(doc["registration_endpoint"]).To(Equal("http://localhost:8080" + oidc.RegistrationEndpoint))
				Expect(doc["code_challenge_methods_supported"]).To(ContainElement("S256"))
				Expect(doc["issuer"]).To(Equal("http://localhost:8080"))
				Expect(doc["authorization_endpoint"]).To(Equal("http://localhost:8080/authorize"))
			})
		}

		ginkgo.It("serves RFC 8414 metadata for a path-suffixed issuer", func() {
			rec := serve(httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp", nil))
			Expect(rec.Code).To(Equal(http.StatusOK))
		})

		ginkgo.It("still serves protected resource metadata", func() {
			rec := serve(httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
			Expect(rec.Code).To(Equal(http.StatusOK))

			var doc map[string]any
			Expect(json.Unmarshal(rec.Body.Bytes(), &doc)).To(Succeed())
			Expect(doc["resource"]).To(HaveSuffix("/mcp"))
		})
	})

	ginkgo.It("completes registration, authorization, consent and code issuance", func() {
		client := registerClient(`{
			"client_name": "Claude Desktop",
			"redirect_uris": ["https://claude.ai/api/mcp/auth_callback"],
			"grant_types": ["authorization_code", "refresh_token"],
			"response_types": ["code"],
			"token_endpoint_auth_method": "none"
		}`)
		clientID := client["client_id"].(string)

		authorizeURL := "/authorize?" + url.Values{
			"client_id":             {clientID},
			"response_type":         {"code"},
			"redirect_uri":          {"https://claude.ai/api/mcp/auth_callback"},
			"scope":                 {"openid profile email offline_access"},
			"state":                 {"state-1"},
			"nonce":                 {"nonce-1"},
			"code_challenge":        {"challenge-1"},
			"code_challenge_method": {"S256"},
			// Claude sends an RFC 8707 resource indicator; it must not break the flow.
			"resource": {"http://localhost:8080/mcp"},
		}.Encode()

		authorizeRec := serve(httptest.NewRequest(http.MethodGet, authorizeURL, nil))
		Expect(authorizeRec.Code).To(Equal(http.StatusFound))

		loginURL := authorizeRec.Header().Get("Location")
		Expect(loginURL).To(ContainSubstring("/oidc/login?auth_request_id="))

		cookies := authorizeRec.Result().Cookies()
		Expect(cookies).ToNot(BeEmpty())
		txCookie := cookies[0]

		parsedLoginURL, err := url.Parse(loginURL)
		Expect(err).ToNot(HaveOccurred())
		authRequestID := parsedLoginURL.Query().Get("auth_request_id")
		Expect(authRequestID).ToNot(BeEmpty())

		// Log in.
		loginReq := httptest.NewRequest(http.MethodPost, "/oidc/login",
			strings.NewReader(url.Values{
				"auth_request_id": {authRequestID},
				"username":        {"admin"},
				"password":        {"admin"},
			}.Encode()))
		loginReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		loginReq.AddCookie(txCookie)
		loginRec := serve(loginReq)

		// A dynamically registered client must divert through consent rather
		// than issuing the code straight away.
		Expect(loginRec.Code).To(Equal(http.StatusFound))
		Expect(loginRec.Header().Get("Location")).To(Equal("/oidc/consent?auth_request_id=" + authRequestID))

		// The consent screen must name the client and show where the code goes.
		consentReq := httptest.NewRequest(http.MethodGet, "/oidc/consent?auth_request_id="+authRequestID, nil)
		consentReq.AddCookie(txCookie)
		consentRec := serve(consentReq)

		Expect(consentRec.Code).To(Equal(http.StatusOK))
		Expect(consentRec.Body.String()).To(ContainSubstring("Claude Desktop"))
		Expect(consentRec.Body.String()).To(ContainSubstring("https://claude.ai/api/mcp/auth_callback"))

		// Approving redirects into the provider's callback, which issues the code.
		approveReq := httptest.NewRequest(http.MethodPost, "/oidc/consent",
			strings.NewReader(url.Values{
				"auth_request_id": {authRequestID},
				"decision":        {"allow"},
			}.Encode()))
		approveReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		approveReq.AddCookie(txCookie)
		approveRec := serve(approveReq)

		Expect(approveRec.Code).To(Equal(http.StatusFound))
		Expect(approveRec.Header().Get("Location")).To(ContainSubstring("/authorize/callback?id=" + authRequestID))

		callbackReq := httptest.NewRequest(http.MethodGet, "/authorize/callback?id="+authRequestID, nil)
		callbackReq.AddCookie(txCookie)
		callbackRec := serve(callbackReq)

		Expect(callbackRec.Code).To(Equal(http.StatusFound))
		redirect, err := url.Parse(callbackRec.Header().Get("Location"))
		Expect(err).ToNot(HaveOccurred())
		Expect(redirect.Scheme + "://" + redirect.Host + redirect.Path).To(Equal("https://claude.ai/api/mcp/auth_callback"))
		Expect(redirect.Query().Get("code")).ToNot(BeEmpty())
		Expect(redirect.Query().Get("state")).To(Equal("state-1"))
	})

	ginkgo.It("exchanges the code and refreshes for a dynamically registered client", func() {
		client := registerClient(`{"client_name":"Codex","redirect_uris":["http://127.0.0.1:1455/auth/callback"]}`)
		clientID := client["client_id"].(string)

		// A real PKCE pair — zitadel mandates PKCE for public clients and
		// verifies the challenge at the token endpoint.
		verifierBytes := make([]byte, 32)
		_, err := rand.Read(verifierBytes)
		Expect(err).ToNot(HaveOccurred())
		verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
		sum := sha256.Sum256([]byte(verifier))
		challenge := base64.RawURLEncoding.EncodeToString(sum[:])

		const redirectURI = "http://127.0.0.1:1455/auth/callback"

		authorizeRec := serve(httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
			"client_id":             {clientID},
			"response_type":         {"code"},
			"redirect_uri":          {redirectURI},
			"scope":                 {"openid profile email offline_access"},
			"state":                 {"state-tok"},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
		}.Encode(), nil))
		Expect(authorizeRec.Code).To(Equal(http.StatusFound))
		txCookie := authorizeRec.Result().Cookies()[0]

		parsedLoginURL, err := url.Parse(authorizeRec.Header().Get("Location"))
		Expect(err).ToNot(HaveOccurred())
		authRequestID := parsedLoginURL.Query().Get("auth_request_id")

		loginReq := httptest.NewRequest(http.MethodPost, "/oidc/login",
			strings.NewReader(url.Values{
				"auth_request_id": {authRequestID}, "username": {"admin"}, "password": {"admin"},
			}.Encode()))
		loginReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		loginReq.AddCookie(txCookie)
		Expect(serve(loginReq).Code).To(Equal(http.StatusFound))

		approveReq := httptest.NewRequest(http.MethodPost, "/oidc/consent",
			strings.NewReader(url.Values{"auth_request_id": {authRequestID}, "decision": {"allow"}}.Encode()))
		approveReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		approveReq.AddCookie(txCookie)
		Expect(serve(approveReq).Code).To(Equal(http.StatusFound))

		callbackReq := httptest.NewRequest(http.MethodGet, "/authorize/callback?id="+authRequestID, nil)
		callbackReq.AddCookie(txCookie)
		callbackRec := serve(callbackReq)
		Expect(callbackRec.Code).To(Equal(http.StatusFound))

		redirect, err := url.Parse(callbackRec.Header().Get("Location"))
		Expect(err).ToNot(HaveOccurred())
		code := redirect.Query().Get("code")
		Expect(code).ToNot(BeEmpty())

		postForm := func(form url.Values) map[string]any {
			req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
			rec := serve(req)

			Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
			var tokens map[string]any
			Expect(json.Unmarshal(rec.Body.Bytes(), &tokens)).To(Succeed())
			return tokens
		}

		tokens := postForm(url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {redirectURI},
			"client_id":     {clientID},
			"code_verifier": {verifier},
		})

		accessToken, _ := tokens["access_token"].(string)
		Expect(accessToken).ToNot(BeEmpty())
		refreshToken, _ := tokens["refresh_token"].(string)
		Expect(refreshToken).ToNot(BeEmpty(), "offline_access must yield a refresh token so clients survive restarts")

		// The access token's audience is the dynamic client_id, which is exactly
		// what authenticateOIDCToken has to accept for /mcp to work.
		claims := decodeJWTClaims(accessToken)
		Expect(audienceHasKnownClient(claims["aud"])).To(BeTrue())
		Expect(claims["iss"]).To(Equal("http://localhost:8080"))

		refreshed := postForm(url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {refreshToken},
			"client_id":     {clientID},
		})
		Expect(refreshed["access_token"]).ToNot(BeEmpty())
		Expect(refreshed["refresh_token"]).ToNot(BeEmpty())
	})

	ginkgo.It("drops the authorization request when consent is denied", func() {
		client := registerClient(`{"client_name":"Sketchy","redirect_uris":["https://claude.ai/api/mcp/auth_callback"]}`)

		authorizeURL := "/authorize?" + url.Values{
			"client_id":             {client["client_id"].(string)},
			"response_type":         {"code"},
			"redirect_uri":          {"https://claude.ai/api/mcp/auth_callback"},
			"scope":                 {"openid"},
			"state":                 {"state-2"},
			"code_challenge":        {"challenge-2"},
			"code_challenge_method": {"S256"},
		}.Encode()

		authorizeRec := serve(httptest.NewRequest(http.MethodGet, authorizeURL, nil))
		Expect(authorizeRec.Code).To(Equal(http.StatusFound))
		txCookie := authorizeRec.Result().Cookies()[0]

		parsedLoginURL, err := url.Parse(authorizeRec.Header().Get("Location"))
		Expect(err).ToNot(HaveOccurred())
		authRequestID := parsedLoginURL.Query().Get("auth_request_id")

		loginReq := httptest.NewRequest(http.MethodPost, "/oidc/login",
			strings.NewReader(url.Values{
				"auth_request_id": {authRequestID}, "username": {"admin"}, "password": {"admin"},
			}.Encode()))
		loginReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		loginReq.AddCookie(txCookie)
		Expect(serve(loginReq).Code).To(Equal(http.StatusFound))

		denyReq := httptest.NewRequest(http.MethodPost, "/oidc/consent",
			strings.NewReader(url.Values{"auth_request_id": {authRequestID}, "decision": {"deny"}}.Encode()))
		denyReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		denyReq.AddCookie(txCookie)
		Expect(serve(denyReq).Code).To(Equal(http.StatusOK))

		var count int64
		Expect(DefaultContext.DB().Table("oidc_auth_requests").Where("id = ?", authRequestID).Count(&count).Error).To(Succeed())
		Expect(count).To(BeZero(), "denied auth request should be deleted so no code can be issued")

		// And the code can no longer be issued.
		callbackReq := httptest.NewRequest(http.MethodGet, "/authorize/callback?id="+authRequestID, nil)
		callbackReq.AddCookie(txCookie)
		Expect(serve(callbackReq).Code).ToNot(Equal(http.StatusFound))
	})

	ginkgo.It("skips consent for the built-in cli client", func() {
		authorizeURL := "/authorize?" + url.Values{
			"client_id":             {oidc.ClientID},
			"response_type":         {"code"},
			"redirect_uri":          {"http://127.0.0.1:5555/callback"},
			"scope":                 {"openid"},
			"state":                 {"state-3"},
			"code_challenge":        {"challenge-3"},
			"code_challenge_method": {"S256"},
		}.Encode()

		authorizeRec := serve(httptest.NewRequest(http.MethodGet, authorizeURL, nil))
		Expect(authorizeRec.Code).To(Equal(http.StatusFound))
		txCookie := authorizeRec.Result().Cookies()[0]

		parsedLoginURL, err := url.Parse(authorizeRec.Header().Get("Location"))
		Expect(err).ToNot(HaveOccurred())
		authRequestID := parsedLoginURL.Query().Get("auth_request_id")

		loginReq := httptest.NewRequest(http.MethodPost, "/oidc/login",
			strings.NewReader(url.Values{
				"auth_request_id": {authRequestID}, "username": {"admin"}, "password": {"admin"},
			}.Encode()))
		loginReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		loginReq.AddCookie(txCookie)
		loginRec := serve(loginReq)

		Expect(loginRec.Code).To(Equal(http.StatusFound))
		Expect(loginRec.Header().Get("Location")).To(ContainSubstring("/authorize/callback"))
	})

	ginkgo.It("rejects an authorization request for an unregistered client", func() {
		authorizeURL := "/authorize?" + url.Values{
			"client_id":     {"not-a-registered-client"},
			"response_type": {"code"},
			"redirect_uri":  {"https://evil.com/cb"},
			"scope":         {"openid"},
		}.Encode()

		Expect(serve(httptest.NewRequest(http.MethodGet, authorizeURL, nil)).Code).To(Equal(http.StatusBadRequest))
	})

	ginkgo.It("accepts tokens issued to a dynamically registered client", func() {
		client := registerClient(`{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"]}`)
		clientID := client["client_id"].(string)

		Expect(audienceHasKnownClient(clientID)).To(BeTrue())
		Expect(audienceHasKnownClient([]any{clientID})).To(BeTrue())
		Expect(audienceHasKnownClient(oidc.ClientID)).To(BeTrue())
		Expect(audienceHasKnownClient("some-other-client")).To(BeFalse())
		Expect(audienceHasKnownClient(nil)).To(BeFalse())
	})
})

var _ = ginkgo.Describe("consent transaction binding", func() {
	ginkgo.It("requires the transaction cookie", func() {
		e := newEchoInstance(DefaultContext)
		Expect(oidc.MountRoutes(e, DefaultContext, "http://localhost:8080", &mockChecker{valid: true}, nil, mockLookup)).To(Succeed())

		serve := func(req *http.Request) int {
			req = req.WithContext(DefaultContext.Wrap(req.Context()))
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			return rec.Code
		}

		Expect(serve(httptest.NewRequest(http.MethodGet, "/oidc/consent?auth_request_id=req-1", nil))).
			To(Equal(http.StatusUnauthorized))

		denyReq := httptest.NewRequest(http.MethodPost, "/oidc/consent",
			strings.NewReader(url.Values{"auth_request_id": {"req-1"}, "decision": {"allow"}}.Encode()))
		denyReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		Expect(serve(denyReq)).To(Equal(http.StatusUnauthorized))
	})
})

var _ = ginkgo.Describe("registration endpoint reachability", func() {
	ginkgo.It("is not behind the auth middleware", func() {
		e := newEchoInstance(DefaultContext)
		e.Use(basicAuthMiddleware)
		e.POST(oidc.RegistrationEndpoint, func(c echo.Context) error {
			return c.NoContent(http.StatusCreated)
		})

		req := httptest.NewRequest(http.MethodPost, oidc.RegistrationEndpoint, strings.NewReader("{}"))
		req = req.WithContext(DefaultContext.Wrap(req.Context()))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusCreated))
	})
})

func decodeJWTClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	Expect(parts).To(HaveLen(3), "access token should be a JWT")

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	Expect(err).ToNot(HaveOccurred())

	var claims map[string]any
	Expect(json.Unmarshal(payload, &claims)).To(Succeed())
	return claims
}
