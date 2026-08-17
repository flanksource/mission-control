package clientapi

import (
	"encoding/json"

	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Connection DTO", func() {
	ginkgo.It("preserves the remote database wire shape", func() {
		id := uuid.New()
		connection := Connection{
			ID:         id,
			Name:       "api",
			Namespace:  "default",
			Source:     SourceUI,
			Type:       ConnectionTypeHTTP,
			URL:        "https://example.com",
			Properties: map[string]string{"bearer": "token"},
		}

		data, err := json.Marshal(connection)
		Expect(err).ToNot(HaveOccurred())
		Expect(data).To(MatchJSON(`{
			"id":"` + id.String() + `",
			"name":"api",
			"namespace":"default",
			"source":"UI",
			"type":"http",
			"url":"https://example.com",
			"properties":{"bearer":"token"},
			"created_at":"0001-01-01T00:00:00Z",
			"updated_at":"0001-01-01T00:00:00Z"
		}`))
	})
})
