package oidc

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Dynamic client registration", func() {
	ginkgo.Describe("validateRedirectURIs", func() {
		valid := []struct {
			name string
			uri  string
		}{
			{"https", "https://claude.ai/api/mcp/auth_callback"},
			{"loopback ipv4 with port", "http://127.0.0.1:33418/callback"},
			{"loopback ipv6", "http://[::1]:6274/oauth/callback"},
			{"localhost", "http://localhost:6274/oauth/callback"},
		}
		for _, tt := range valid {
			ginkgo.It("accepts "+tt.name, func() {
				got, err := validateRedirectURIs([]string{tt.uri})
				Expect(err).ToNot(HaveOccurred())
				Expect(got).To(Equal([]string{tt.uri}))
			})
		}

		invalid := []struct {
			name string
			uri  string
		}{
			{"plain http on a public host", "http://evil.com/cb"},
			{"custom scheme", "myapp://callback"},
			{"javascript scheme", "javascript:alert(1)"},
			{"fragment", "https://evil.com/cb#foo"},
			{"no host", "https:///cb"},
		}
		for _, tt := range invalid {
			ginkgo.It("rejects "+tt.name, func() {
				_, err := validateRedirectURIs([]string{tt.uri})
				Expect(err).To(HaveOccurred())
			})
		}

		ginkgo.It("requires at least one redirect uri", func() {
			_, err := validateRedirectURIs(nil)
			Expect(err).To(HaveOccurred())
		})

		ginkgo.It("caps the number of redirect uris", func() {
			uris := make([]string, maxRedirectURIs+1)
			for i := range uris {
				uris[i] = "https://example.com/cb"
			}
			_, err := validateRedirectURIs(uris)
			Expect(err).To(HaveOccurred())
		})
	})

	ginkgo.Describe("client_id encoding", func() {
		ginkgo.It("round-trips metadata", func() {
			md := clientMetadata{
				Name:         "Claude",
				RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
				IssuedAt:     1700000000,
			}

			id, err := encodeClientID(md)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(HavePrefix(dynamicClientIDPrefix))

			decoded, err := decodeClientID(id)
			Expect(err).ToNot(HaveOccurred())
			Expect(decoded.Name).To(Equal("Claude"))
			Expect(decoded.RedirectURIs).To(Equal(md.RedirectURIs))
		})

		ginkgo.It("is stable across calls so restarts do not invalidate clients", func() {
			md := clientMetadata{Name: "Codex", RedirectURIs: []string{"http://127.0.0.1:1455/auth/callback"}, IssuedAt: 42}

			first, err := encodeClientID(md)
			Expect(err).ToNot(HaveOccurred())
			second, err := encodeClientID(md)
			Expect(err).ToNot(HaveOccurred())
			Expect(first).To(Equal(second))
		})

		// The encoding is unsigned by design, so the guarantee is not "cannot be
		// forged" but "a forgery can express no more than POST /register would
		// have handed out".
		ginkgo.It("re-validates redirect uris on decode", func() {
			payload, err := json.Marshal(clientMetadata{
				Name:         "Evil",
				RedirectURIs: []string{"http://evil.com/cb"},
			})
			Expect(err).ToNot(HaveOccurred())

			forged := dynamicClientIDPrefix + base64.RawURLEncoding.EncodeToString(payload)
			_, err = decodeClientID(forged)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must use https"))
		})

		ginkgo.It("rejects ids that are not dynamically registered", func() {
			_, err := decodeClientID(ClientID)
			Expect(err).To(HaveOccurred())
		})

		ginkgo.It("rejects oversized ids", func() {
			_, err := decodeClientID(dynamicClientIDPrefix + strings.Repeat("a", maxClientIDLength))
			Expect(err).To(HaveOccurred())
		})

		ginkgo.It("rejects malformed base64", func() {
			_, err := decodeClientID(dynamicClientIDPrefix + "!!!not-base64!!!")
			Expect(err).To(HaveOccurred())
		})
	})

	ginkgo.Describe("IsKnownClient", func() {
		ginkgo.It("accepts the built-in cli client", func() {
			Expect(IsKnownClient(ClientID)).To(BeTrue())
		})

		ginkgo.It("accepts a dynamically registered client", func() {
			id, err := encodeClientID(clientMetadata{RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"}})
			Expect(err).ToNot(HaveOccurred())
			Expect(IsKnownClient(id)).To(BeTrue())
		})

		ginkgo.It("rejects anything else", func() {
			Expect(IsKnownClient("some-other-client")).To(BeFalse())
		})
	})

	ginkgo.Describe("POST "+RegistrationEndpoint, func() {
		register := func(body string) *httptest.ResponseRecorder {
			e := echo.New()
			mountRegistrationRoutes(e)

			req := httptest.NewRequest(http.MethodPost, RegistrationEndpoint, strings.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			return rec
		}

		ginkgo.It("registers a public client and never issues a secret", func() {
			rec := register(`{
				"client_name": "Claude",
				"redirect_uris": ["https://claude.ai/api/mcp/auth_callback"],
				"grant_types": ["authorization_code", "refresh_token"],
				"response_types": ["code"],
				"token_endpoint_auth_method": "none"
			}`)

			Expect(rec.Code).To(Equal(http.StatusCreated))

			var resp map[string]any
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp).ToNot(HaveKey("client_secret"))
			Expect(resp["token_endpoint_auth_method"]).To(Equal("none"))
			Expect(resp["client_id"]).To(HavePrefix(dynamicClientIDPrefix))

			decoded, err := decodeClientID(resp["client_id"].(string))
			Expect(err).ToNot(HaveOccurred())
			Expect(decoded.RedirectURIs).To(ConsistOf("https://claude.ai/api/mcp/auth_callback"))
		})

		ginkgo.It("rejects a non-loopback http redirect uri", func() {
			rec := register(`{"redirect_uris": ["http://evil.com/cb"]}`)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))

			var resp registrationError
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Error).To(Equal("invalid_redirect_uri"))
		})

		ginkgo.It("rejects a request for a confidential client", func() {
			rec := register(`{"redirect_uris": ["https://example.com/cb"], "token_endpoint_auth_method": "client_secret_post"}`)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))

			var resp registrationError
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Error).To(Equal("invalid_client_metadata"))
		})

		ginkgo.It("rejects an unsupported grant type", func() {
			rec := register(`{"redirect_uris": ["https://example.com/cb"], "grant_types": ["implicit"]}`)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
		})

		ginkgo.It("rejects a malformed body", func() {
			rec := register(`not json`)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
		})
	})

	ginkgo.Describe("requiresConsent", func() {
		ginkgo.It("skips consent for the built-in cli client", func() {
			Expect(requiresConsent(ClientID)).To(BeFalse())
		})

		ginkgo.It("requires consent for a dynamically registered client", func() {
			id, err := encodeClientID(clientMetadata{RedirectURIs: []string{"https://claude.ai/cb"}})
			Expect(err).ToNot(HaveOccurred())
			Expect(requiresConsent(id)).To(BeTrue())
		})
	})
})
