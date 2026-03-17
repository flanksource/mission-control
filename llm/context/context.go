package context

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/commons/duration"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
)

// AddAnalysis adds the given analyses to the context.
// It converts each ConfigAnalysis to an Analysis and adds it to the appropriate Config.
func (t *Context) AddAnalysis(analyses ...models.ConfigAnalysis) {
	for _, analysis := range analyses {
		var a Analysis
		a.FromModel(analysis)

		for i, config := range t.Configs {
			if analysis.ConfigID.String() == config.ID {
				t.Configs[i].Analyses = append(t.Configs[i].Analyses, a)
			}
		}
	}
}

// AddChanges adds the given changes to the context.
// It converts each ConfigChangeRow to a Change and adds it to the appropriate Config.
func (t *Context) AddChanges(changes ...query.ConfigChangeRow) {
	for _, change := range changes {
		var c Change
		c.FromModel(change)

		for i, config := range t.Configs {
			if change.ConfigID == config.ID {
				t.Configs[i].Changes = append(t.Configs[i].Changes, c)
			}
		}
	}
}

// Create builds a context for the given context.
// It fetches the main config and its relationships, changes, and analyses.
func Create(ctx context.Context, spec api.LLMContextRequest) (*Context, error) {
	var kg Context

	var config models.ConfigItem
	if err := ctx.DB().Where("id = ?", spec.Config).Find(&config).Error; err != nil {
		return nil, fmt.Errorf("failed to get config (%s): %w", spec.Config, err)
	} else if config.ID == uuid.Nil {
		return nil, fmt.Errorf("config doesn't exist (%s)", spec.Config)
	} else {
		ci := Config{
			ID:          config.ID.String(),
			Name:        lo.FromPtr(config.Name),
			Type:        lo.FromPtr(config.Type),
			Config:      lo.FromPtr(config.Config),
			Created:     config.CreatedAt,
			Updated:     config.UpdatedAt,
			Health:      config.Health,
			Status:      config.Status,
			Description: config.Description,
			Labels:      config.Labels,
			Tags:        config.Tags,
			Deleted:     config.DeletedAt,
		}

		if config.Config != nil && *config.Config != "" {
			var m map[string]any
			if err := json.Unmarshal([]byte(*config.Config), &m); err != nil {
				return nil, err
			}
			ci.Config = m
		}

		kg.Configs = append(kg.Configs, ci)
	}

	for _, relationship := range spec.Relationships {
		err := kg.processRelationship(ctx, config.ID, relationship)
		if err != nil {
			return nil, err
		}
	}

	if spec.ShouldFetchConfigChanges() {
		changes, err := query.FindCatalogChanges(ctx, query.CatalogChangesSearchRequest{
			CatalogID: config.ID.String(),
			Recursive: query.CatalogChangeRecursiveNone,
			From:      fmt.Sprintf("now-%s", spec.Changes.Since),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get config changes (%s): %w", config.ID, err)
		}

		kg.AddChanges(changes.Changes...)
	}

	if spec.Analysis != nil && spec.Analysis.Since != "" {
		analyses, err := getConfigAnalysis(ctx, config.ID.String(), spec.Analysis.Since)
		if err != nil {
			return nil, err
		}
		kg.AddAnalysis(analyses...)
	}

	return &kg, nil
}

// processRelationship processes a relationship for the context.
// It fetches related configs, adds them to the graph, and fetches their changes and analyses if requested.
func (t *Context) processRelationship(ctx context.Context, configID uuid.UUID, relationship api.LLMContextRequestRelationship) error {
	relatedConfigs, err := query.GetRelatedConfigs(ctx, relationship.ToRelationshipQuery(configID))
	if err != nil {
		return fmt.Errorf("failed to get related config (%s): %w", configID, err)
	}

	relatedConfigIDs := lo.Map(relatedConfigs, func(c query.RelatedConfig, _ int) string {
		return c.ID.String()
	})

	for _, rc := range relatedConfigs {
		t.Configs = append(t.Configs, Config{
			ID:      rc.ID.String(),
			Name:    rc.Name,
			Type:    rc.Type,
			Created: rc.CreatedAt,
			Updated: &rc.UpdatedAt,
			Deleted: rc.DeletedAt,
			Health:  rc.Health,
			Status:  rc.Status,
		})

		for _, relatedID := range rc.RelatedIDs {
			if !lo.Contains(relatedConfigIDs, relatedID) {
				continue
			}

			t.Edges = append(t.Edges, Edge{
				From: rc.ID.String(),
				To:   relatedID,
			})
		}
	}

	if relationship.Changes.Since != "" {
		changes, err := query.FindCatalogChanges(ctx, query.CatalogChangesSearchRequest{
			CatalogID: configID.String(),
			Depth:     lo.FromPtr(relationship.Depth),
			Recursive: relationship.Direction.ToChangeDirection(),
			From:      fmt.Sprintf("now-%s", relationship.Changes.Since),
		})
		if err != nil {
			return fmt.Errorf("failed to get config changes (%s): %w", configID, err)
		}

		t.AddChanges(changes.Changes...)
	}

	if relationship.Analysis.Since != "" {
		analysis, err := getConfigAnalysis(ctx, configID.String(), relationship.Analysis.Since)
		if err != nil {
			return fmt.Errorf("failed to get config analyses (%s): %w", configID, err)
		}

		t.AddAnalysis(analysis...)
	}

	return nil
}

// getConfigAnalysis fetches configuration analyses for the given config ID and time period.
// The 'since' parameter is a duration string (e.g., "24h", "7d") that specifies how far back to look.
func getConfigAnalysis(ctx context.Context, configID, since string) ([]models.ConfigAnalysis, error) {
	parsed, err := duration.ParseDuration(since)
	if err != nil {
		return nil, fmt.Errorf("failed to parse duration for analysis (%s): %w", since, err)
	}

	var analyses []models.ConfigAnalysis
	if err := ctx.DB().
		Where("NOW() - last_observed < ?", time.Duration(parsed)).
		Where("config_id = ?", configID).
		Find(&analyses).Error; err != nil {
		return nil, fmt.Errorf("failed to get config analysis: %w", err)
	}

	return analyses, nil
}
