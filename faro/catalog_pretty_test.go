package main

import (
	"encoding/json"
	"time"

	"github.com/flanksource/incident-commander/clientapi"
	clicky "github.com/flanksource/incident-commander/clientcli"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("faro catalog pretty output", func() {
	ginkgo.It("shows complete properties and additional config metadata", func() {
		name := "api"
		typ := "Kubernetes::Deployment"
		status := "Running"
		description := "production API"
		source := "kubernetes"
		scraper := "cluster-prod"
		parentID := uuid.New()
		updatedAt := time.Date(2026, 7, 20, 12, 30, 0, 0, time.UTC)
		configJSON := `{"apiVersion":"apps/v1","spec":{"replicas":3}}`
		zero := int64(0)
		max := int64(10)
		labels := map[string]string{"app": "api"}
		costPerMinute := 0.00125
		costTotal1h := 0.075
		costTotal1d := 1.8
		costTotal30d := 54.0

		item := clientapi.ConfigItem{
			ID:          uuid.New(),
			ScraperID:   &scraper,
			AgentID:     uuid.New(),
			ConfigClass: "Deployment",
			ExternalID:  []string{"default/api"},
			Type:        &typ,
			Status:      &status,
			Ready:       true,
			Name:        &name,
			Description: &description,
			Config:      &configJSON,
			Source:      &source,
			ParentID:    &parentID,
			Path:        "cluster/default/api",
			Labels:      &labels,
			Tags:          map[string]string{"environment": "production"},
			Properties: &clientapi.CatalogProperties{
				{Label: "Namespace", Text: "default"},
				{Name: "restart_count", Value: &zero, Max: &max, Unit: "restarts", Status: "stable"},
				{Name: "documentation", Links: []clientapi.CatalogLink{{URL: "https://example.com/api"}}},
				{Name: "empty_property"},
			},
			CreatedAt:  time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC),
			InsertedAt: time.Date(2026, 7, 1, 8, 1, 0, 0, time.UTC),
			UpdatedAt:  &updatedAt,
		}

		output := (catalogItemDetail{
			ConfigItem: item,
			Summary: &clientapi.ConfigItemSummary{
				CostPerMinute: &costPerMinute,
				CostTotal1h:   &costTotal1h,
				CostTotal1d:   &costTotal1d,
				CostTotal30d:  &costTotal30d,
			},
		}).Pretty().String()

		for _, expected := range []string{
			"production API",
			"cluster-prod",
			"cluster/default/api",
			"default/api",
			"$0.001250",
			"$0.07",
			"$1.80",
			"$54.00",
			"2026-07-01T08:00:00Z",
			"Properties",
			"Namespace",
			"default",
			"restart_count",
			"0/10 restarts (stable)",
			"https://example.com/api",
			"empty_property",
			"replicas",
		} {
			Expect(output).To(ContainSubstring(expected))
		}
	})

	ginkgo.It("preserves the ConfigItem JSON and YAML shapes", func() {
		name := "api"
		typ := "Kubernetes::Pod"
		item := clientapi.ConfigItem{ID: uuid.New(), Name: &name, Type: &typ, ConfigClass: "Pod"}
		cost := 54.0
		detail := catalogItemDetail{
			ConfigItem: item,
			Summary:    &clientapi.ConfigItemSummary{CostTotal30d: &cost},
		}

		original, err := json.Marshal(item)
		Expect(err).ToNot(HaveOccurred())
		wrapped, err := json.Marshal(detail)
		Expect(err).ToNot(HaveOccurred())

		Expect(wrapped).To(MatchJSON(original))

		originalYAML, err := clicky.Format(item, clicky.FormatOptions{YAML: true})
		Expect(err).ToNot(HaveOccurred())
		wrappedYAML, err := clicky.Format(detail, clicky.FormatOptions{YAML: true})
		Expect(err).ToNot(HaveOccurred())

		Expect(wrappedYAML).To(MatchYAML(originalYAML))
		Expect(wrappedYAML).ToNot(ContainSubstring("configitem:"))
	})
})
