package cmd

import (
	"github.com/flanksource/incident-commander/clientapi"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Connection client adapter", func() {
	ginkgo.It("round trips connection fields", func() {
		input := clientapi.Connection{
			ID:          uuid.New(),
			Name:        "api",
			Namespace:   "default",
			Source:      clientapi.SourceUI,
			Type:        clientapi.ConnectionTypeHTTP,
			URL:         "https://example.com",
			Username:    "user",
			Password:    "secret",
			Properties:  map[string]string{"bearer": "token"},
			Certificate: "certificate",
			InsecureTLS: true,
		}

		Expect(connectionFromModel(connectionToModel(input))).To(Equal(input))
	})
})
