package sdk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("catalog projection client", func() {
	ginkgo.It("lists config-scoped catalog changes with exact-count filters", func() {
		since := time.Date(2026, 8, 1, 10, 30, 0, 0, time.UTC)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodGet))
			Expect(r.URL.Path).To(Equal("/db/catalog_changes"))
			Expect(r.Header.Get("Prefer")).To(Equal("count=exact"))
			Expect(r.URL.Query().Get("select")).To(Equal("*"))
			Expect(r.URL.Query().Get("order")).To(Equal("created_at.desc"))
			Expect(r.URL.Query().Get("change_type")).To(Equal("in.(PermissionAdded,PermissionRemoved)"))
			Expect(r.URL.Query().Get("source")).To(Equal("in.(config-db)"))
			Expect(r.URL.Query().Get("created_at")).To(Equal("gte.2026-08-01T10:30:00Z"))
			Expect(r.URL.Query().Get("limit")).To(Equal("25"))
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Range", "0-0/1")
			_, _ = w.Write([]byte(`[{"id":"521bae33-e4c3-42eb-a9c5-071ab92940b5","config_id":"21e7586d-31fb-453c-a205-d73dc6b58eaa","change_type":"PermissionAdded","source":"config-db"}]`))
		}))
		defer server.Close()

		changes, total, err := New(server.URL, "tok").ListCatalogChanges(context.Background(), CatalogChangeOptions{
			ChangeTypes: []string{"PermissionRemoved", "PermissionAdded"},
			Sources:     []string{"config-db"},
			Since:       &since,
			Limit:       25,
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(total).To(Equal(1))
		Expect(changes).To(HaveLen(1))
		Expect(changes[0].ConfigID).To(Equal("21e7586d-31fb-453c-a205-d73dc6b58eaa"))
	})
})
