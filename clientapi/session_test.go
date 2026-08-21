package clientapi

import (
	"encoding/base64"
	"encoding/json"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("browser session data", func() {
	ginkgo.It("decodes the client-visible JWT claims", func() {
		expires := time.Now().Add(time.Hour).Unix()
		payload, err := json.Marshal(map[string]any{
			"aud": "graph.microsoft.com",
			"sub": "user-1",
			"scp": "User.Read AuditLog.Read.All",
			"exp": expires,
		})
		Expect(err).NotTo(HaveOccurred())
		token := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"

		claims := DecodeJWT(token)

		Expect(claims).NotTo(BeNil())
		Expect(claims.Audience).To(Equal("graph.microsoft.com"))
		Expect(claims.Subject).To(Equal("user-1"))
		Expect(claims.Scopes).To(Equal("User.Read AuditLog.Read.All"))
		Expect(claims.ExpiresAt.Unix()).To(Equal(expires))
		Expect(claims.Raw).To(Equal(token))
	})

	ginkgo.It("builds portable state from MSAL storage", func() {
		payload := base64.RawURLEncoding.EncodeToString([]byte(`{"aud":"api.example.com","exp":4102444800}`))
		state := NewPlaywrightSessionState(
			Cookies{{Name: "session", Domain: "example.com"}},
			map[string]string{"accesstoken-key": `{"secret":"header.` + payload + `.signature"}`},
			nil,
			"https://example.com/path",
		)

		Expect(state.Cookies).To(HaveLen(1))
		Expect(state.Tokens).To(HaveLen(1))
		Expect(state.Tokens[0].Audience).To(Equal("api.example.com"))
		Expect(state.Origins).To(Equal([]SessionOrigin{{Origin: "https://example.com"}}))
	})
})
