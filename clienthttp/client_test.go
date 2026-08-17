package clienthttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("client HTTP transport", func() {
	ginkgo.It("sends JSON requests with client headers and query parameters", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodPost))
			Expect(r.URL.Path).To(Equal("/items"))
			Expect(r.URL.Query().Get("limit")).To(Equal("5"))
			Expect(r.Header.Get("Authorization")).To(Equal("Bearer token"))
			Expect(r.Header.Get("User-Agent")).To(Equal("faro-test"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer server.Close()

		response, err := NewClient().
			BaseURL(server.URL).
			Header("Authorization", "Bearer token").
			UserAgent("faro-test").
			R(context.Background()).
			QueryParam("limit", "5").
			Post("/items", map[string]any{"name": "api"})

		Expect(err).ToNot(HaveOccurred())
		Expect(response.IsOK()).To(BeTrue())
		body, err := response.AsString()
		Expect(err).ToNot(HaveOccurred())
		Expect(body).To(MatchJSON(`{"ok":true}`))
	})

	ginkgo.It("captures HAR bodies without consuming them and redacts secrets", func() {
		collector := NewHARCollector(HARConfig{CaptureContentTypes: []string{"application/json"}})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"response-secret","value":"ok"}`))
		}))
		defer server.Close()

		client := NewClient().
			BaseURL(server.URL).
			Header("Content-Type", "application/json").
			Header("Authorization", "Bearer request-secret")
		restore := SetHARCollector(collector)
		defer restore()

		response, err := client.
			R(context.Background()).
			QueryParam("token", "query-secret").
			Post("/items", map[string]any{"password": "body-secret", "name": "api"})
		Expect(err).ToNot(HaveOccurred())
		body, err := response.AsString()
		Expect(err).ToNot(HaveOccurred())
		Expect(body).To(MatchJSON(`{"access_token":"response-secret","value":"ok"}`))

		entries := collector.Entries()
		Expect(entries).To(HaveLen(1))
		Expect(entries[0].Request.URL).ToNot(ContainSubstring("query-secret"))
		Expect(entries[0].Request.PostData).ToNot(BeNil())
		Expect(entries[0].Request.PostData.Text).To(MatchJSON(`{"password":"***","name":"api"}`))
		Expect(entries[0].Response.Content.Text).To(MatchJSON(`{"access_token":"***","value":"ok"}`))

		var authorization string
		for _, header := range entries[0].Request.Headers {
			if strings.EqualFold(header.Name, "Authorization") {
				authorization = header.Value
			}
		}
		Expect(authorization).To(Equal("***"))
	})

	ginkgo.It("redacts OAuth forms, API keys, and session parameters", func() {
		collector := NewHARCollector(HARConfig{CaptureContentTypes: []string{"application/x-www-form-urlencoded"}})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer server.Close()

		restore := SetHARCollector(collector)
		defer restore()
		response, err := NewClient().
			Header("Content-Type", "application/x-www-form-urlencoded").
			Header("X-API-Key", "api-secret").
			R(context.Background()).
			QueryParam("sessionid", "session-secret").
			Post(server.URL, "grant_type=authorization_code&code=login-secret&code_verifier=pkce-secret&refresh_token=refresh-secret")

		Expect(err).ToNot(HaveOccurred())
		_, err = response.AsString()
		Expect(err).ToNot(HaveOccurred())

		entries := collector.Entries()
		Expect(entries).To(HaveLen(1))
		form, err := url.ParseQuery(entries[0].Request.PostData.Text)
		Expect(err).ToNot(HaveOccurred())
		Expect(form.Get("grant_type")).To(Equal("authorization_code"))
		Expect(form.Get("code")).To(Equal("***"))
		Expect(form.Get("code_verifier")).To(Equal("***"))
		Expect(form.Get("refresh_token")).To(Equal("***"))
		Expect(entries[0].Request.URL).ToNot(ContainSubstring("session-secret"))
		Expect(entries[0].Request.Headers).To(ContainElement(HARHeader{Name: "X-Api-Key", Value: "***"}))
	})

	ginkgo.It("redacts complete JSON bodies before truncating HAR content", func() {
		payload := `{"password":"oversized-secret","padding":"` + strings.Repeat("x", 256) + `"}`
		collector := NewHARCollector(HARConfig{MaxBodySize: 64, CaptureContentTypes: []string{"application/json"}})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
		}))
		defer server.Close()

		restore := SetHARCollector(collector)
		defer restore()
		response, err := NewClient().R(context.Background()).Get(server.URL)
		Expect(err).ToNot(HaveOccurred())
		body, err := response.AsString()
		Expect(err).ToNot(HaveOccurred())
		Expect(body).To(Equal(payload))

		entries := collector.Entries()
		Expect(entries).To(HaveLen(1))
		Expect(entries[0].Response.Content.Text).ToNot(ContainSubstring("oversized-secret"))
		Expect(entries[0].Response.Content.Truncated).To(BeTrue())
		Expect(entries[0].Response.Content.Size).To(Equal(int64(len(payload))))
		Expect(entries[0].Response.BodySize).To(Equal(int64(len(payload))))
	})
})
