package main

import (
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/incident-commander/clientapi"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func toonTestItem(name string) catalogItem {
	typ := "Kubernetes::Pod"
	status := "Running"
	return catalogItem(clientapi.ConfigItem{
		ID:          uuid.New(),
		ConfigClass: "Pod",
		Type:        &typ,
		Status:      &status,
		Name:        &name,
		CreatedAt:   time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC),
		InsertedAt:  time.Date(2026, 7, 1, 8, 1, 0, 0, time.UTC),
	})
}

// clicky owns the TOON encoder; what faro has to keep true is that its own
// catalog types survive it — their uuids, pointer fields and custom
// marshallers all go through json hooks the encoder would otherwise not call.
var _ = ginkgo.Describe("faro toon output", func() {
	ginkgo.It("renders catalog items as a tabular block keyed by json field names", func() {
		items := []catalogItem{toonTestItem("api"), toonTestItem("web")}

		out, err := clicky.Format(catalogListItems(items), clicky.FormatOptions{Format: "toon"})
		Expect(err).ToNot(HaveOccurred())

		header, _, found := strings.Cut(out, "\n")
		Expect(found).To(BeTrue())
		Expect(header).To(HavePrefix("[2]{"))
		for _, field := range []string{"id", "config_class", "name", "status", "type"} {
			Expect(header).To(ContainSubstring(field))
		}
		Expect(out).ToNot(ContainSubstring("omitempty"))

		// catalogListItem adds _id through MarshalJSON, which the encoder only
		// ever sees because values are decoded through encoding/json first.
		Expect(header).To(ContainSubstring("_id"))

		// A uuid.UUID is [16]byte; without its MarshalJSON it renders as sixteen
		// numbers and silently pushes every later column out of its header slot.
		Expect(out).To(ContainSubstring(items[0].ID.String()))
		Expect(out).To(ContainSubstring("api"))
		Expect(out).To(ContainSubstring("web"))
	})

	ginkgo.It("renders a single item with its nested maps", func() {
		item := toonTestItem("api")
		item.Tags = map[string]string{"environment": "production"}

		out, err := clicky.Format(catalogItemDetail{ConfigItem: clientapi.ConfigItem(item)}, clicky.FormatOptions{Format: "toon"})
		Expect(err).ToNot(HaveOccurred())

		Expect(out).To(ContainSubstring("config_class: Pod"))
		Expect(out).To(ContainSubstring("tags:\n  environment: production"))
	})
})
