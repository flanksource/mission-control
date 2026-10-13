package echo

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	echov4 "github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/db"
)

var _ = ginkgo.Describe("Connection credentials in /db", func() {
	const password = "db-connection-password"

	row := func() map[string]any {
		return map[string]any{
			"id":          uuid.NewString(),
			"name":        "db",
			"username":    "secret://db/username",
			"password":    password,
			"certificate": "",
			"url":         "postgres://app:" + password + "@db.example.com/app",
			"properties":  map[string]any{"bearer": "token", "region": "eu-west-1"},
		}
	}

	respond := func(rawURL, contentType string, payload any) *http.Response {
		ginkgo.GinkgoHelper()
		body, err := json.Marshal(payload)
		Expect(err).ToNot(HaveOccurred())
		u, err := url.Parse(rawURL)
		Expect(err).ToNot(HaveOccurred())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{echov4.HeaderContentType: []string{contentType}},
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    &http.Request{Method: http.MethodGet, URL: u},
		}
	}

	read := func(resp *http.Response) string {
		ginkgo.GinkgoHelper()
		body, err := io.ReadAll(resp.Body)
		Expect(err).ToNot(HaveOccurred())
		return string(body)
	}

	expectMasked := func(r any) {
		ginkgo.GinkgoHelper()
		m := r.(map[string]any)
		Expect(m).To(HaveKeyWithValue("username", "secret://db/username"))
		Expect(m).To(HaveKeyWithValue("password", db.MaskedValue))
		Expect(m).To(HaveKeyWithValue("url", "postgres://app:***@db.example.com/app"))
		Expect(m["properties"]).To(HaveKeyWithValue("bearer", db.MaskedValue))
		Expect(m["properties"]).To(HaveKeyWithValue("region", "eu-west-1"))
	}

	ginkgo.It("masks the credentials of connection rows", func() {
		resp := respond("http://postgrest/connections?select=*", "application/json; charset=utf-8", []any{row()})
		Expect(maskConnectionCredentials(resp)).To(Succeed())

		var rows []any
		Expect(json.Unmarshal([]byte(read(resp)), &rows)).To(Succeed())
		expectMasked(rows[0])
	})

	ginkgo.It("masks the credentials of a single connection object", func() {
		resp := respond("http://postgrest/connections?id=eq.1", "application/vnd.pgrst.object+json", row())
		Expect(maskConnectionCredentials(resp)).To(Succeed())

		var obj any
		Expect(json.Unmarshal([]byte(read(resp)), &obj)).To(Succeed())
		expectMasked(obj)
	})

	ginkgo.It("masks the credentials of connections embedded in another table's rows", func() {
		resp := respond("http://postgrest/notifications?select=id,conn:connections(*),playbook:playbooks(id,connections(*))", "application/json",
			[]any{map[string]any{"id": "n1", "conn": row(), "playbook": map[string]any{"id": "p1", "connections": []any{row()}}}})
		Expect(maskConnectionCredentials(resp)).To(Succeed())

		body := read(resp)
		Expect(body).ToNot(ContainSubstring(password))

		var rows []map[string]any
		Expect(json.Unmarshal([]byte(body), &rows)).To(Succeed())
		expectMasked(rows[0]["conn"])
		expectMasked(rows[0]["playbook"].(map[string]any)["connections"].([]any)[0])
	})

	ginkgo.It("leaves other tables alone", func() {
		resp := respond("http://postgrest/playbooks?select=*", "application/json", []any{map[string]any{"password": password}})
		Expect(maskConnectionCredentials(resp)).To(Succeed())
		Expect(read(resp)).To(ContainSubstring(password))
	})

	ginkgo.It("refuses connections in a format it can't mask", func() {
		resp := respond("http://postgrest/connections", "text/csv", "name,password\ndb,"+password)
		Expect(maskConnectionCredentials(resp)).To(Succeed())
		Expect(resp.StatusCode).To(Equal(http.StatusNotAcceptable))
		Expect(read(resp)).ToNot(ContainSubstring(password))
	})

	for _, tt := range []struct {
		table, query string
		refused      bool
	}{
		{"connections", "select=*", false},
		{"connections", "select=id,name,password,properties", false},
		{"connections", "name=eq.password&order=created_at.desc", false},
		{"connections", "select=pw:password", true},
		{"connections", "select=password::text", true},
		{"connections", "select=properties->>bearer", true},
		{"connections", "select=password.count()", true},
		{"connections", "password=like.a*", true},
		{"connections", "properties->>bearer=eq.x", true},
		{"connections", "or=(name.eq.a,password.like.a*)", true},
		{"connections", "order=password.asc", true},
		{"notifications", "url=eq.x&select=*,conn:connections(id,name)", false},
		{"notifications", "select=*,conn:connections(secret:password)", true},
		{"notifications", "select=*,...connections(name)", true},
		{"notifications", "select=*,conn:connections(*)&conn.password=like.a*", true},
		{"notifications", "select=*,connections(*)&connections.or=(password.eq.x)", true},
	} {
		ginkgo.It(tt.table+"?"+tt.query, func() {
			query, err := url.ParseQuery(tt.query)
			Expect(err).ToNot(HaveOccurred())
			_, err = parseConnectionQuery(tt.table, query)
			if tt.refused {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		})
	}

	ginkgo.It("puts the stored credentials back into a write that sends them masked", func() {
		stored := models.Connection{
			ID:          uuid.New(),
			Name:        "db-connection-write",
			Namespace:   "default",
			Source:      models.SourceUI,
			Type:        models.ConnectionTypePostgres,
			URL:         "postgres://app:" + password + "@db.example.com/app",
			Password:    password,
			Certificate: "certificate",
			Properties:  types.JSONStringMap{"bearer": "token", "region": "eu-west-1"},
		}
		Expect(DefaultContext.DB().Create(&stored).Error).To(Succeed())
		ginkgo.DeferCleanup(func() { Expect(DefaultContext.DB().Delete(&stored).Error).To(Succeed()) })

		masked := db.MaskConnection(stored)
		body, err := json.Marshal(map[string]any{
			"name":        "renamed",
			"url":         masked.URL,
			"password":    masked.Password,
			"certificate": masked.Certificate,
			"properties":  map[string]any{"bearer": db.MaskedValue, "region": "us-east-1"},
		})
		Expect(err).ToNot(HaveOccurred())

		req := httptest.NewRequest(http.MethodPatch, "/db/connections?id=eq."+stored.ID.String(), bytes.NewReader(body))
		req = req.WithContext(DefaultContext)
		var forwarded map[string]any
		handler := connectionCredentialsInterceptor(func(c echov4.Context) error {
			return json.NewDecoder(c.Request().Body).Decode(&forwarded)
		})
		Expect(handler(echov4.New().NewContext(req, httptest.NewRecorder()))).To(Succeed())

		Expect(forwarded).To(HaveKeyWithValue("name", "renamed"))
		Expect(forwarded).To(HaveKeyWithValue("url", stored.URL))
		Expect(forwarded).To(HaveKeyWithValue("password", password))
		Expect(forwarded).To(HaveKeyWithValue("certificate", "certificate"))
		Expect(forwarded["properties"]).To(HaveKeyWithValue("bearer", "token"))
		Expect(forwarded["properties"]).To(HaveKeyWithValue("region", "us-east-1"))
	})
})
