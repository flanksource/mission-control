package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/clientcmd"
	"github.com/flanksource/incident-commander/sdk"
)

type projectionSourceResult struct {
	Items    []map[string]any    `json:"items" yaml:"items"`
	Context  map[string]any      `json:"context" yaml:"context"`
	Warnings []ProjectionWarning `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

func requireCompleteProjection(resource string, returned, total, limit int) error {
	if total < 0 {
		return fmt.Errorf("%s query did not report an exact total; projection completeness cannot be verified", resource)
	}
	if total > returned {
		return fmt.Errorf("%s query returned %d of %d rows; increase the projection limit above %d", resource, returned, total, limit)
	}
	return nil
}

// projectionSourceKind names the one source query a projection declares, for progress
// output. validate() guarantees exactly one is set.
func projectionSourceKind(projection Projection) string {
	switch query := projection.Spec.Source.Query; {
	case query.Configs != nil:
		return "configs"
	case query.IdentityAccess != nil:
		return "identity access"
	case query.Changes != nil:
		return "changes"
	case query.Insights != nil:
		return "insights"
	default:
		return "no source"
	}
}

func runProjectionQuery(projection Projection) (projectionSourceResult, error) {
	contextName, err := accessContextName()
	if err != nil {
		return projectionSourceResult{}, err
	}
	now := time.Now().UTC()
	result := projectionSourceResult{Context: map[string]any{
		"name":        contextName,
		"observed_at": now.Format(time.RFC3339),
		"date":        now.Format(registerDateLayout),
	}}

	switch query := projection.Spec.Source.Query; {
	case query.Configs != nil:
		result.Items, err = queryConfigProjection(*query.Configs)
	case query.IdentityAccess != nil:
		result.Items, result.Warnings, err = queryIdentityAccessProjection(*query.IdentityAccess, result.Context)
	case query.Changes != nil:
		result.Items, err = queryChangeProjection(*query.Changes, now)
	case query.Insights != nil:
		result.Items, err = queryInsightProjection(*query.Insights)
	default:
		err = fmt.Errorf("projection %s contains no source query", projection.Metadata.Name)
	}
	if err != nil {
		return projectionSourceResult{}, fmt.Errorf("projection %s: %w", projection.Metadata.Name, err)
	}
	return result, nil
}

func queryConfigProjection(config ProjectionConfigsQuery) ([]map[string]any, error) {
	client, err := clientcmd.RemoteClient()
	if err != nil {
		return nil, err
	}

	response, err := client.SearchCatalog(context.Background(), query.SearchResourcesRequest{
		Limit:      config.Limit,
		Timestamps: true,
		Configs:    []types.ResourceSelector{configProjectionSelector(config)},
	})
	if err != nil {
		return nil, err
	}
	if len(response.Configs) >= config.Limit {
		return nil, fmt.Errorf("configs query reached limit %d; increase spec.source.query.configs.limit", config.Limit)
	}
	ids := make([]string, 0, len(response.Configs))
	for _, selected := range response.Configs {
		ids = append(ids, selected.ID)
	}

	items, err := client.GetCatalogItems(context.Background(), ids)
	if err != nil {
		return nil, err
	}
	if len(items) != len(ids) {
		return nil, fmt.Errorf("catalog changed while projecting: selected %d configs but hydrated %d", len(ids), len(items))
	}

	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		mapped, err := configProjectionMap(item)
		if err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	sort.Slice(result, func(i, j int) bool { return fmt.Sprint(result[i]["id"]) < fmt.Sprint(result[j]["id"]) })
	return result, nil
}

func configProjectionSelector(config ProjectionConfigsQuery) types.ResourceSelector {
	agent := config.Agent
	if agent == "" {
		agent = "all"
	}
	return types.ResourceSelector{
		Search:        strings.TrimSpace(config.Search),
		Agent:         agent,
		Types:         types.Items(config.ConfigTypes),
		TagSelector:   config.TagSelector,
		LabelSelector: config.LabelSelector,
		FieldSelector: config.FieldSelector,
	}
}

func configProjectionMap(item models.ConfigItem) (map[string]any, error) {
	mapped, err := projectionMap(item)
	if err != nil {
		return nil, err
	}
	config := map[string]any{}
	if item.Config != nil && strings.TrimSpace(*item.Config) != "" {
		if err := json.Unmarshal([]byte(*item.Config), &config); err != nil {
			return nil, fmt.Errorf("catalog item %s config: %w", item.ID, err)
		}
	}
	mapped["config"] = config
	mapped["properties_by_name"] = projectionPropertiesByName(item.Properties)
	return mapped, nil
}

func projectionPropertiesByName(properties *types.Properties) map[string]any {
	byName := map[string]any{}
	if properties == nil {
		return byName
	}
	for _, property := range *properties {
		if property == nil || property.Name == "" {
			continue
		}
		if property.Value != nil {
			byName[property.Name] = *property.Value
		} else {
			byName[property.Name] = property.Text
		}
	}
	return byName
}

func queryInsightProjection(config ProjectionInsightsQuery) ([]map[string]any, error) {
	agent := config.Agent
	if agent == "" {
		agent = "all"
	}
	// limit bounds the whole result, not one request: remoteSearchInsights pages past the
	// server's per-request cap. A projection wants every matching insight, so an omitted
	// limit means unbounded rather than a silently truncated finding set.
	result, err := remoteSearchInsights(config.Search, agent, config.Limit)
	if err != nil {
		return nil, err
	}
	if result.Limited {
		return nil, fmt.Errorf("insights query returned the requested %d rows and more remain; raise or remove spec.source.query.insights.limit", config.Limit)
	}
	if len(result.Details) != len(result.Items) {
		return nil, fmt.Errorf("catalog changed while projecting: selected %d insights but hydrated %d", len(result.Items), len(result.Details))
	}

	items := make([]map[string]any, 0, len(result.Details))
	for _, detail := range result.Details {
		mapped, err := projectionMap(detail)
		if err != nil {
			return nil, err
		}
		mapped["properties_by_name"] = projectionPropertiesByName(detail.Properties)
		items = append(items, mapped)
	}
	sort.Slice(items, func(i, j int) bool { return fmt.Sprint(items[i]["id"]) < fmt.Sprint(items[j]["id"]) })
	return items, nil
}

func queryIdentityAccessProjection(config ProjectionIdentityAccessQuery, projectionContext map[string]any) ([]map[string]any, []ProjectionWarning, error) {
	export, err := buildAccessExport(accessExportOptions{Limit: config.Limit, RequireComplete: true, UserTypes: config.UserTypes})
	if err != nil {
		return nil, nil, err
	}
	contextName := fmt.Sprint(projectionContext["name"])
	items := make([]map[string]any, 0, len(export.Entries))
	for _, entry := range export.Entries {
		mapped, err := projectionMap(entry)
		if err != nil {
			return nil, nil, err
		}
		if _, ok := mapped["tenant"]; !ok {
			mapped["tenant"] = ""
		}
		for _, field := range []string{"groups", "config_access"} {
			rows, _ := mapped[field].([]any)
			for _, raw := range rows {
				if row, ok := raw.(map[string]any); ok {
					row["context"] = contextName
				}
			}
		}
		items = append(items, mapped)
	}
	return items, export.Warnings, nil
}

func queryChangeProjection(config ProjectionChangesQuery, now time.Time) ([]map[string]any, error) {
	client, err := clientcmd.RemoteClient()
	if err != nil {
		return nil, err
	}
	var since *time.Time
	if config.Since != "" {
		parsed, _ := time.Parse(time.RFC3339, config.Since)
		since = &parsed
	} else if config.Lookback != "" {
		duration, _ := time.ParseDuration(config.Lookback)
		parsed := now.Add(-duration)
		since = &parsed
	}
	changes, total, err := client.ListCatalogChanges(context.Background(), sdk.CatalogChangeOptions{
		ChangeTypes: config.ChangeTypes,
		Sources:     config.Sources,
		Since:       since,
		Limit:       config.Limit,
	})
	if err != nil {
		return nil, err
	}
	if err := requireCompleteProjection("changes", len(changes), total, config.Limit); err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(changes))
	for _, change := range changes {
		mapped, err := projectionMap(change)
		if err != nil {
			return nil, err
		}
		items = append(items, mapped)
	}
	return items, nil
}

func projectionMap(value any) (map[string]any, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var mapped map[string]any
	if err := json.Unmarshal(body, &mapped); err != nil {
		return nil, err
	}
	return mapped, nil
}
