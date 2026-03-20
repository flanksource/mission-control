package auth

import (
	gocontext "context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"time"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth/oidc"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	oidclib "github.com/zitadel/oidc/v3/pkg/oidc"
)

var _ = ginkgo.Describe("OIDC", func() {
	var (
		keyPath  string
		person   models.Person
		provider *oidc.Provider
	)

	ginkgo.BeforeEach(func() {
		ensureOIDCTables(DefaultContext)

		dir := ginkgo.GinkgoT().TempDir()
		keyPath = fmt.Sprintf("%s/oidc.pem", dir)

		person = models.Person{
			ID:    uuid.New(),
			Name:  "oidctest",
			Email: "oidctest@example.com",
		}
		Expect(DefaultContext.DB().Create(&person).Error).To(Succeed())

		var err error
		provider, err = oidc.NewProvider(DefaultContext, "http://localhost:8080", keyPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(provider).ToNot(BeNil())
	})

	ginkgo.AfterEach(func() {
		DefaultContext.DB().Where("id = ?", person.ID).Delete(&models.Person{})
		DefaultContext.DB().Exec("DELETE FROM oidc_auth_requests")
		DefaultContext.DB().Exec("DELETE FROM oidc_refresh_tokens")
	})

	ginkgo.It("creates signing key file on first start", func() {
		_, err := os.Stat(keyPath)
		Expect(err).ToNot(HaveOccurred(), "signing key file should have been created")
	})

	ginkgo.It("reuses the same key on second load", func() {
		dir := ginkgo.GinkgoT().TempDir()
		keyPath2 := fmt.Sprintf("%s/oidc.pem", dir)

		data, err := os.ReadFile(keyPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(keyPath2, data, 0600)).To(Succeed())

		provider2, err := oidc.NewProvider(DefaultContext, "http://localhost:8080", keyPath2)
		Expect(err).ToNot(HaveOccurred())
		Expect(provider2).ToNot(BeNil())
	})

	ginkgo.It("stores public key in oidc_public_keys table", func() {
		var count int64
		Expect(DefaultContext.DB().Model(&oidc.PublicKey{}).Count(&count).Error).To(Succeed())
		Expect(count).To(BeNumerically(">=", 1))
	})

	ginkgo.Describe("Bearer token validation", func() {
		ginkgo.It("rejects a non-JWT string", func() {
			c := newEchoContext(DefaultContext)
			authenticated, err := authenticateOIDCToken(c, "not-a-jwt")
			Expect(err).ToNot(HaveOccurred())
			Expect(authenticated).To(BeFalse())
		})

		ginkgo.It("rejects a JWT with wrong issuer", func() {
			// Syntactically valid JWT but wrong issuer
			wrongJWT := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9." +
				"eyJzdWIiOiJ0ZXN0IiwiaXNzIjoid3JvbmctaXNzdWVyIiwiYXVkIjoibWMtY2xpIn0." +
				"invalidsig"

			c := newEchoContext(DefaultContext)
			authenticated, err := authenticateOIDCToken(c, wrongJWT)
			Expect(err).ToNot(HaveOccurred())
			Expect(authenticated).To(BeFalse())
		})
	})

	ginkgo.Describe("Storage", func() {
		ginkgo.It("creates and retrieves auth requests", func() {
			req := &oidclib.AuthRequest{
				ClientID:     oidc.ClientID,
				RedirectURI:  "http://localhost:9999/callback",
				Scopes:       []string{"openid", "profile"},
				State:        "test-state",
				Nonce:        "test-nonce",
				ResponseType: "code",
			}
			ar, err := provider.Storage.CreateAuthRequest(gocontext.TODO(), req, "")
			Expect(err).ToNot(HaveOccurred())
			Expect(ar.GetID()).ToNot(BeEmpty())
			Expect(ar.GetClientID()).To(Equal(oidc.ClientID))
			Expect(ar.GetState()).To(Equal("test-state"))
			Expect(ar.GetNonce()).To(Equal("test-nonce"))

			fetched, err := provider.Storage.AuthRequestByID(gocontext.TODO(), ar.GetID())
			Expect(err).ToNot(HaveOccurred())
			Expect(fetched.GetID()).To(Equal(ar.GetID()))
		})

		ginkgo.It("saves auth code and retrieves by code", func() {
			req := &oidclib.AuthRequest{
				ClientID:     oidc.ClientID,
				RedirectURI:  "http://localhost:9999/callback",
				Scopes:       []string{"openid"},
				ResponseType: "code",
			}
			ar, err := provider.Storage.CreateAuthRequest(gocontext.TODO(), req, "")
			Expect(err).ToNot(HaveOccurred())

			Expect(provider.Storage.SaveAuthCode(gocontext.TODO(), ar.GetID(), "test-code-123")).To(Succeed())

			byCode, err := provider.Storage.AuthRequestByCode(gocontext.TODO(), "test-code-123")
			Expect(err).ToNot(HaveOccurred())
			Expect(byCode.GetID()).To(Equal(ar.GetID()))
			Expect(byCode.Done()).To(BeTrue())
		})

		ginkgo.It("deletes auth requests", func() {
			req := &oidclib.AuthRequest{
				ClientID:     oidc.ClientID,
				RedirectURI:  "http://localhost:9999/callback",
				Scopes:       []string{"openid"},
				ResponseType: "code",
			}
			ar, err := provider.Storage.CreateAuthRequest(gocontext.TODO(), req, "")
			Expect(err).ToNot(HaveOccurred())

			Expect(provider.Storage.DeleteAuthRequest(gocontext.TODO(), ar.GetID())).To(Succeed())

			_, err = provider.Storage.AuthRequestByID(gocontext.TODO(), ar.GetID())
			Expect(err).To(HaveOccurred())
		})

		ginkgo.It("sets subject on auth request", func() {
			req := &oidclib.AuthRequest{
				ClientID:     oidc.ClientID,
				RedirectURI:  "http://localhost:9999/callback",
				Scopes:       []string{"openid"},
				ResponseType: "code",
			}
			ar, err := provider.Storage.CreateAuthRequest(gocontext.TODO(), req, "")
			Expect(err).ToNot(HaveOccurred())

			Expect(provider.Storage.SetAuthRequestSubject(ar.GetID(), person.ID.String())).To(Succeed())

			fetched, err := provider.Storage.AuthRequestByID(gocontext.TODO(), ar.GetID())
			Expect(err).ToNot(HaveOccurred())
			Expect(fetched.GetSubject()).To(Equal(person.ID.String()))
			Expect(fetched.GetAuthTime()).ToNot(BeZero())
		})

		ginkgo.It("creates and retrieves refresh tokens", func() {
			req := &oidclib.AuthRequest{
				ClientID:     oidc.ClientID,
				RedirectURI:  "http://localhost:9999/callback",
				Scopes:       []string{"openid"},
				ResponseType: "code",
			}
			ar, err := provider.Storage.CreateAuthRequest(gocontext.TODO(), req, "")
			Expect(err).ToNot(HaveOccurred())
			Expect(provider.Storage.SetAuthRequestSubject(ar.GetID(), person.ID.String())).To(Succeed())

			fetched, err := provider.Storage.AuthRequestByID(gocontext.TODO(), ar.GetID())
			Expect(err).ToNot(HaveOccurred())

			accessID, refreshToken, expiry, err := provider.Storage.CreateAccessAndRefreshTokens(gocontext.TODO(), fetched, "")
			Expect(err).ToNot(HaveOccurred())
			Expect(accessID).ToNot(BeEmpty())
			Expect(refreshToken).ToNot(BeEmpty())
			Expect(expiry).To(BeTemporally(">", time.Now()))

			rt, err := provider.Storage.TokenRequestByRefreshToken(gocontext.TODO(), refreshToken)
			Expect(err).ToNot(HaveOccurred())
			Expect(rt.GetSubject()).To(Equal(person.ID.String()))
		})

		ginkgo.It("revokes refresh tokens", func() {
			req := &oidclib.AuthRequest{
				ClientID:     oidc.ClientID,
				RedirectURI:  "http://localhost:9999/callback",
				Scopes:       []string{"openid"},
				ResponseType: "code",
			}
			ar, err := provider.Storage.CreateAuthRequest(gocontext.TODO(), req, "")
			Expect(err).ToNot(HaveOccurred())
			Expect(provider.Storage.SetAuthRequestSubject(ar.GetID(), person.ID.String())).To(Succeed())
			fetched, err := provider.Storage.AuthRequestByID(gocontext.TODO(), ar.GetID())
			Expect(err).ToNot(HaveOccurred())

			_, refreshToken, _, err := provider.Storage.CreateAccessAndRefreshTokens(gocontext.TODO(), fetched, "")
			Expect(err).ToNot(HaveOccurred())

			oidcErr := provider.Storage.RevokeToken(gocontext.TODO(), refreshToken, "", "")
			Expect(oidcErr).To(BeNil())

			_, err = provider.Storage.TokenRequestByRefreshToken(gocontext.TODO(), refreshToken)
			Expect(err).To(HaveOccurred())
		})

		ginkgo.It("cleans up expired auth requests and refresh tokens", func() {
			DefaultContext.DB().Exec("INSERT INTO oidc_auth_requests (id, client_id, redirect_uri, response_type, expires_at) VALUES (?, ?, ?, ?, ?)",
				"expired-ar", oidc.ClientID, "http://localhost/cb", "code", time.Now().Add(-1*time.Hour))
			DefaultContext.DB().Exec("INSERT INTO oidc_refresh_tokens (id, token, client_id, subject, auth_time, rotation_id, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
				"expired-rt", "expired-token", oidc.ClientID, person.ID.String(), time.Now(), "rot-1", time.Now().Add(-1*time.Hour))

			Expect(provider.Storage.CleanupExpired()).To(Succeed())

			_, err := provider.Storage.AuthRequestByID(gocontext.TODO(), "expired-ar")
			Expect(err).To(HaveOccurred())
			_, err = provider.Storage.TokenRequestByRefreshToken(gocontext.TODO(), "expired-token")
			Expect(err).To(HaveOccurred())
		})

		ginkgo.It("returns correct client for known client ID", func() {
			client, err := provider.Storage.GetClientByClientID(gocontext.TODO(), oidc.ClientID)
			Expect(err).ToNot(HaveOccurred())
			Expect(client.GetID()).To(Equal(oidc.ClientID))
		})

		ginkgo.It("rejects unknown client ID", func() {
			_, err := provider.Storage.GetClientByClientID(gocontext.TODO(), "unknown-client")
			Expect(err).To(HaveOccurred())
		})

		ginkgo.It("returns signing key and key set", func() {
			sk, err := provider.Storage.SigningKey(gocontext.TODO())
			Expect(err).ToNot(HaveOccurred())
			Expect(sk.ID()).ToNot(BeEmpty())

			ks, err := provider.Storage.KeySet(gocontext.TODO())
			Expect(err).ToNot(HaveOccurred())
			Expect(len(ks)).To(BeNumerically(">=", 1))
		})
	})

	ginkgo.Describe("Token validation with real JWT", func() {
		ginkgo.It("accepts a valid JWT signed by the provider's key", func() {
			// Read the private key that the provider generated
			keyData, err := os.ReadFile(keyPath)
			Expect(err).ToNot(HaveOccurred())
			privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(keyData)
			Expect(err).ToNot(HaveOccurred())

			// Clear the cache so it reloads from DB
			oidcPublicKeyCache.Flush()

			issuer := "http://localhost:8080"
			savedPublicURL := api.PublicURL
			api.PublicURL = issuer
			defer func() { api.PublicURL = savedPublicURL }()

			claims := jwt.MapClaims{
				"iss": issuer,
				"aud": oidc.ClientID,
				"sub": person.ID.String(),
				"exp": time.Now().Add(time.Hour).Unix(),
				"iat": time.Now().Unix(),
			}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
			tokenStr, err := token.SignedString(privateKey)
			Expect(err).ToNot(HaveOccurred())

			c := newEchoContext(DefaultContext)
			authenticated, err := authenticateOIDCToken(c, tokenStr)
			Expect(err).ToNot(HaveOccurred())
			Expect(authenticated).To(BeTrue())
		})

		ginkgo.It("rejects a JWT signed by a different key", func() {
			wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
			Expect(err).ToNot(HaveOccurred())

			oidcPublicKeyCache.Flush()
			savedPublicURL := api.PublicURL
			api.PublicURL = "http://localhost:8080"
			defer func() { api.PublicURL = savedPublicURL }()

			claims := jwt.MapClaims{
				"iss": "http://localhost:8080",
				"aud": oidc.ClientID,
				"sub": person.ID.String(),
				"exp": time.Now().Add(time.Hour).Unix(),
			}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
			tokenStr, err := token.SignedString(wrongKey)
			Expect(err).ToNot(HaveOccurred())

			c := newEchoContext(DefaultContext)
			authenticated, err := authenticateOIDCToken(c, tokenStr)
			Expect(err).ToNot(HaveOccurred())
			Expect(authenticated).To(BeFalse())
		})

		ginkgo.It("rejects an expired JWT", func() {
			keyData, err := os.ReadFile(keyPath)
			Expect(err).ToNot(HaveOccurred())
			privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(keyData)
			Expect(err).ToNot(HaveOccurred())

			oidcPublicKeyCache.Flush()
			savedPublicURL := api.PublicURL
			api.PublicURL = "http://localhost:8080"
			defer func() { api.PublicURL = savedPublicURL }()

			claims := jwt.MapClaims{
				"iss": "http://localhost:8080",
				"aud": oidc.ClientID,
				"sub": person.ID.String(),
				"exp": time.Now().Add(-1 * time.Hour).Unix(),
			}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
			tokenStr, err := token.SignedString(privateKey)
			Expect(err).ToNot(HaveOccurred())

			c := newEchoContext(DefaultContext)
			authenticated, err := authenticateOIDCToken(c, tokenStr)
			Expect(err).ToNot(HaveOccurred())
			Expect(authenticated).To(BeFalse())
		})

		ginkgo.It("rejects a JWT with wrong audience", func() {
			keyData, err := os.ReadFile(keyPath)
			Expect(err).ToNot(HaveOccurred())
			privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(keyData)
			Expect(err).ToNot(HaveOccurred())

			oidcPublicKeyCache.Flush()
			savedPublicURL := api.PublicURL
			api.PublicURL = "http://localhost:8080"
			defer func() { api.PublicURL = savedPublicURL }()

			claims := jwt.MapClaims{
				"iss": "http://localhost:8080",
				"aud": "wrong-client",
				"sub": person.ID.String(),
				"exp": time.Now().Add(time.Hour).Unix(),
			}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
			tokenStr, err := token.SignedString(privateKey)
			Expect(err).ToNot(HaveOccurred())

			c := newEchoContext(DefaultContext)
			authenticated, err := authenticateOIDCToken(c, tokenStr)
			Expect(err).ToNot(HaveOccurred())
			Expect(authenticated).To(BeFalse())
		})

		ginkgo.It("rejects a JWT with unknown subject", func() {
			keyData, err := os.ReadFile(keyPath)
			Expect(err).ToNot(HaveOccurred())
			privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(keyData)
			Expect(err).ToNot(HaveOccurred())

			oidcPublicKeyCache.Flush()
			savedPublicURL := api.PublicURL
			api.PublicURL = "http://localhost:8080"
			defer func() { api.PublicURL = savedPublicURL }()

			claims := jwt.MapClaims{
				"iss": "http://localhost:8080",
				"aud": oidc.ClientID,
				"sub": uuid.New().String(), // non-existent person
				"exp": time.Now().Add(time.Hour).Unix(),
			}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
			tokenStr, err := token.SignedString(privateKey)
			Expect(err).ToNot(HaveOccurred())

			c := newEchoContext(DefaultContext)
			authenticated, err := authenticateOIDCToken(c, tokenStr)
			Expect(err).ToNot(HaveOccurred())
			Expect(authenticated).To(BeFalse())
		})
	})

	ginkgo.Describe("LoginHandler", func() {
		ginkgo.It("renders login form with auth_request_id", func() {
			login := oidc.NewLoginHandler(provider.Storage, provider.OpenIDProvider, &mockChecker{}, mockLookup)
			e := newEchoInstance(DefaultContext)
			req := httptest.NewRequest(http.MethodGet, "/oidc/login?auth_request_id=test-123", nil)
			req = req.WithContext(DefaultContext.Wrap(req.Context()))
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(login.ShowForm(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Body.String()).To(ContainSubstring("test-123"))
			Expect(rec.Body.String()).To(ContainSubstring("Sign in"))
		})

		ginkgo.It("returns 400 when auth_request_id is missing", func() {
			login := oidc.NewLoginHandler(provider.Storage, provider.OpenIDProvider, &mockChecker{}, mockLookup)
			e := newEchoInstance(DefaultContext)
			req := httptest.NewRequest(http.MethodGet, "/oidc/login", nil)
			req = req.WithContext(DefaultContext.Wrap(req.Context()))
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(login.ShowForm(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
		})

		ginkgo.It("rejects invalid credentials", func() {
			login := oidc.NewLoginHandler(provider.Storage, provider.OpenIDProvider, &mockChecker{valid: false}, mockLookup)
			e := newEchoInstance(DefaultContext)

			form := url.Values{
				"auth_request_id": {"req-1"},
				"username":        {"baduser"},
				"password":        {"badpass"},
			}
			req := httptest.NewRequest(http.MethodPost, "/oidc/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req = req.WithContext(DefaultContext.Wrap(req.Context()))
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(login.HandleSubmit(c)).To(Succeed())
			Expect(rec.Body.String()).To(ContainSubstring("Invalid credentials"))
		})
	})

	ginkgo.Describe("basicAuthMiddleware", ginkgo.Ordered, func() {
		var testServer *httptest.Server

		ginkgo.BeforeAll(func() {
			OIDCEnabled = true
			OIDCSigningKeyPath = keyPath
			HtpasswdFile = ""

			e := newEchoInstance(DefaultContext)
			e.Use(basicAuthMiddleware)
			e.GET("/whoami", func(c echo.Context) error {
				ctx := c.Request().Context().(dutyContext.Context)
				if ctx.User() == nil {
					return c.String(http.StatusUnauthorized, "no user")
				}
				return c.String(http.StatusOK, ctx.User().Name)
			})
			testServer = httptest.NewServer(e)
		})

		ginkgo.AfterAll(func() {
			OIDCEnabled = false
			if testServer != nil {
				testServer.Close()
			}
		})

		ginkgo.It("rejects requests with no credentials", func() {
			resp, err := http.Get(testServer.URL + "/whoami")
			Expect(err).ToNot(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})
	})
})

// ensureOIDCTables creates the OIDC tables if they don't exist.
// These will be in duty's migrations in production; we create them inline for tests.
func ensureOIDCTables(ctx dutyContext.Context) {
	ddl := `
CREATE TABLE IF NOT EXISTS oidc_public_keys (
	id          TEXT PRIMARY KEY,
	algorithm   TEXT NOT NULL DEFAULT 'RS256',
	public_key  BYTEA NOT NULL,
	created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	expires_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS oidc_auth_requests (
	id                   TEXT PRIMARY KEY,
	client_id            TEXT NOT NULL,
	redirect_uri         TEXT NOT NULL,
	scopes               TEXT[] NOT NULL DEFAULT '{}',
	state                TEXT,
	nonce                TEXT,
	response_type        TEXT NOT NULL,
	code_challenge       TEXT,
	code_challenge_method TEXT,
	subject              TEXT,
	auth_time            TIMESTAMPTZ,
	code                 TEXT,
	done                 BOOLEAN NOT NULL DEFAULT FALSE,
	created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	expires_at           TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS oidc_refresh_tokens (
	id          TEXT PRIMARY KEY,
	token       TEXT NOT NULL UNIQUE,
	client_id   TEXT NOT NULL,
	subject     TEXT NOT NULL,
	scopes      TEXT[] NOT NULL DEFAULT '{}',
	auth_time   TIMESTAMPTZ NOT NULL,
	rotation_id TEXT NOT NULL,
	created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	expires_at  TIMESTAMPTZ NOT NULL
);`
	Expect(ctx.DB().Exec(ddl).Error).To(Succeed())
}

func newEchoContext(ctx dutyContext.Context) echo.Context {
	e := newEchoInstance(ctx)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(ctx.Wrap(req.Context()))
	return e.NewContext(req, httptest.NewRecorder())
}

func newEchoInstance(ctx dutyContext.Context) *echo.Echo {
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.SetRequest(c.Request().WithContext(ctx.Wrap(c.Request().Context())))
			return next(c)
		}
	})
	return e
}

type mockChecker struct {
	valid bool
}

func (m *mockChecker) Match(_, _ string) bool { return m.valid }

var mockLookup = func(ctx dutyContext.Context, user string) (string, error) {
	return uuid.New().String(), nil
}
