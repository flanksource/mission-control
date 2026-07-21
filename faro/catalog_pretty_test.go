package main

import (
	"encoding/json"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
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
		labels := types.JSONStringMap{"app": "api"}

		item := models.ConfigItem{
			ID:            uuid.New(),
			ScraperID:     &scraper,
			AgentID:       uuid.New(),
			ConfigClass:   "Deployment",
			ExternalID:    []string{"default/api"},
			Type:          &typ,
			Status:        &status,
			Ready:         true,
			Name:          &name,
			Description:   &description,
			Config:        &configJSON,
			Source:        &source,
			ParentID:      &parentID,
			Path:          "cluster/default/api",
			CostPerMinute: 0.00125,
			CostTotal1d:   1.8,
			CostTotal7d:   12.6,
			CostTotal30d:  54,
			Labels:        &labels,
			Tags:          types.JSONStringMap{"environment": "production"},
			Properties: &types.Properties{
				{Label: "Namespace", Text: "default"},
				{Name: "restart_count", Value: &zero, Max: &max, Unit: "restarts", Status: "stable"},
				{Name: "documentation", Links: []types.Link{{URL: "https://example.com/api"}}},
				{Name: "empty_property"},
			},
			CreatedAt:  time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC),
			InsertedAt: time.Date(2026, 7, 1, 8, 1, 0, 0, time.UTC),
			UpdatedAt:  &updatedAt,
		}

		output := (catalogItemDetail{ConfigItem: item}).Pretty().String()

		for _, expected := range []string{
			"production API",
			"cluster-prod",
			"cluster/default/api",
			"default/api",
			"Cost per Minute",
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

	ginkgo.It("preserves the ConfigItem JSON shape", func() {
		name := "api"
		typ := "Kubernetes::Pod"
		item := models.ConfigItem{ID: uuid.New(), Name: &name, Type: &typ, ConfigClass: "Pod"}

		original, err := json.Marshal(item)
		Expect(err).ToNot(HaveOccurred())
		wrapped, err := json.Marshal(catalogItemDetail{ConfigItem: item})
		Expect(err).ToNot(HaveOccurred())

		Expect(wrapped).To(MatchJSON(original))
	})
})
