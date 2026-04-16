package senders

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/models"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("ForConnection", func() {
	for _, tt := range []struct {
		connType string
	}{
		{models.ConnectionTypeTelegram},
		{models.ConnectionTypeDiscord},
		{models.ConnectionTypeTeams},
		{models.ConnectionTypeMattermost},
		{models.ConnectionTypeNtfy},
		{models.ConnectionTypePushbullet},
		{models.ConnectionTypePushover},
	} {
		ginkgo.It("returns sender for "+tt.connType, func() {
			sender, err := ForConnection(&models.Connection{Type: tt.connType})
			Expect(err).ToNot(HaveOccurred())
			Expect(sender).ToNot(BeNil())
		})
	}

	ginkgo.It("returns error for unsupported type", func() {
		_, err := ForConnection(&models.Connection{Type: "unknown"})
		Expect(err).To(HaveOccurred())
	})
})

var _ = ginkgo.Describe("Teams sender", func() {
	ginkgo.It("sends MessageCard to webhook", func() {
		var received map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodPost))
			Expect(r.Header.Get("Content-Type")).To(Equal("application/json"))
			body, _ := io.ReadAll(r.Body)
			Expect(json.Unmarshal(body, &received)).To(Succeed())
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		conn := &models.Connection{Type: models.ConnectionTypeTeams, URL: server.URL}
		err := (&Teams{}).Send(context.TODO(), conn, Data{Title: "Alert", Message: "Server is down"})
		Expect(err).ToNot(HaveOccurred())
		Expect(received["@type"]).To(Equal("MessageCard"))
		sections := received["sections"].([]any)
		Expect(sections).To(HaveLen(1))
		section := sections[0].(map[string]any)
		Expect(section["activityTitle"]).To(Equal("Alert"))
		Expect(section["text"]).To(Equal("Server is down"))
	})

	ginkgo.It("returns error on non-200 response", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("bad request"))
		}))
		defer server.Close()

		conn := &models.Connection{Type: models.ConnectionTypeTeams, URL: server.URL}
		err := (&Teams{}).Send(context.TODO(), conn, Data{Title: "Test", Message: "msg"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("400"))
	})
})

var _ = ginkgo.Describe("Mattermost sender", func() {
	ginkgo.It("sends payload to webhook", func() {
		var received mattermostPayload
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodPost))
			body, _ := io.ReadAll(r.Body)
			Expect(json.Unmarshal(body, &received)).To(Succeed())
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		conn := &models.Connection{Type: models.ConnectionTypeMattermost, URL: server.URL, Username: "bot"}
		err := (&Mattermost{}).Send(context.TODO(), conn, Data{Title: "Deploy", Message: "v1.2.3 deployed"})
		Expect(err).ToNot(HaveOccurred())
		Expect(received.Username).To(Equal("bot"))
		Expect(received.Text).To(ContainSubstring("Deploy"))
		Expect(received.Text).To(ContainSubstring("v1.2.3 deployed"))
	})

	ginkgo.It("returns error on non-200 response", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		conn := &models.Connection{Type: models.ConnectionTypeMattermost, URL: server.URL}
		err := (&Mattermost{}).Send(context.TODO(), conn, Data{Message: "test"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("500"))
	})
})

var _ = ginkgo.Describe("Discord sender", func() {
	ginkgo.It("sends embed to webhook", func() {
		var received discordWebhookPayload
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodPost))
			Expect(r.Header.Get("Content-Type")).To(Equal("application/json"))
			body, _ := io.ReadAll(r.Body)
			Expect(json.Unmarshal(body, &received)).To(Succeed())
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		conn := &models.Connection{Type: models.ConnectionTypeDiscord, URL: server.URL}
		err := (&Discord{}).Send(context.TODO(), conn, Data{Title: "Alert", Message: "CPU high"})
		Expect(err).ToNot(HaveOccurred())
		Expect(received.Embeds).To(HaveLen(1))
		Expect(received.Embeds[0].Title).To(Equal("Alert"))
		Expect(received.Embeds[0].Description).To(Equal("CPU high"))
	})
})

var _ = ginkgo.Describe("Ntfy sender", func() {
	ginkgo.It("sends message with title header", func() {
		var receivedTitle string
		var receivedBody string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedTitle = r.Header.Get("Title")
			body, _ := io.ReadAll(r.Body)
			receivedBody = string(body)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		conn := &models.Connection{Type: models.ConnectionTypeNtfy, URL: server.URL, Username: "test-topic"}
		err := (&Ntfy{}).Send(context.TODO(), conn, Data{Title: "Alert", Message: "disk full"})
		Expect(err).ToNot(HaveOccurred())
		Expect(receivedTitle).To(Equal("Alert"))
		Expect(receivedBody).To(Equal("disk full"))
	})
})

var _ = ginkgo.Describe("Pushbullet sender", func() {
	ginkgo.It("sends note to API", func() {
		var received map[string]string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodPost))
			Expect(r.Header.Get("Access-Token")).To(Equal("test-token"))
			body, _ := io.ReadAll(r.Body)
			Expect(json.Unmarshal(body, &received)).To(Succeed())
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		origClient := httpClient
		httpClient = &http.Client{Transport: &rewriteTransport{server.URL, http.DefaultTransport}}
		defer func() { httpClient = origClient }()

		conn := &models.Connection{Type: models.ConnectionTypePushbullet, Password: "test-token"}
		err := (&Pushbullet{}).Send(context.TODO(), conn, Data{Title: "Alert", Message: "CPU high"})
		Expect(err).ToNot(HaveOccurred())
		Expect(received["type"]).To(Equal("note"))
		Expect(received["title"]).To(Equal("Alert"))
		Expect(received["body"]).To(Equal("CPU high"))
	})
})

var _ = ginkgo.Describe("Pushover sender", func() {
	ginkgo.It("sends message to API", func() {
		var receivedToken, receivedUser, receivedTitle, receivedMessage string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodPost))
			r.ParseForm()
			receivedToken = r.FormValue("token")
			receivedUser = r.FormValue("user")
			receivedTitle = r.FormValue("title")
			receivedMessage = r.FormValue("message")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		origClient := httpClient
		httpClient = &http.Client{Transport: &rewriteTransport{server.URL, http.DefaultTransport}}
		defer func() { httpClient = origClient }()

		conn := &models.Connection{Type: models.ConnectionTypePushover, Password: "app-token", Username: "user-key"}
		err := (&Pushover{}).Send(context.TODO(), conn, Data{Title: "Alert", Message: "Server down"})
		Expect(err).ToNot(HaveOccurred())
		Expect(receivedToken).To(Equal("app-token"))
		Expect(receivedUser).To(Equal("user-key"))
		Expect(receivedTitle).To(Equal("Alert"))
		Expect(receivedMessage).To(Equal("Server down"))
	})
})

// rewriteTransport redirects all requests to a local test server.
type rewriteTransport struct {
	targetURL string
	base      http.RoundTripper
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.targetURL[len("http://"):]
	return t.base.RoundTrip(req)
}
