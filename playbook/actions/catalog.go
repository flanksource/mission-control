package actions

import (
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

type Catalog struct{}

type CatalogResult struct {
	ID          uuid.UUID         `json:"id"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	ConfigClass string            `json:"config_class,omitempty"`
	Config      string            `json:"config,omitempty"`
	Health      string            `json:"health,omitempty"`
	Status      string            `json:"status,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	ScraperID   string            `json:"scraper_id"`
}

func (c *Catalog) Run(ctx context.Context, action v1.CatalogAction) (*CatalogResult, error) {
	scraperID, err := resolveScraperID(ctx, action.Scraper)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve scraper %q: %w", action.Scraper, err)
	}

	// Enforce uniqueness on (name, scraper_id, type)
	var existing models.ConfigItem
	query := ctx.DB().Where("name = ? AND type = ? AND deleted_at IS NULL", action.Name, action.Type)
	if scraperID == "" {
		query = query.Where("scraper_id IS NULL")
	} else {
		query = query.Where("scraper_id = ?", scraperID)
	}
	if err := query.First(&existing).Error; err == nil {
		return nil, fmt.Errorf("config item with name=%q, scraper_id=%s, type=%q already exists (id=%s)", action.Name, scraperID, action.Type, existing.ID)
	}

	item := models.ConfigItem{
		ID:          uuid.New(),
		Name:        lo.ToPtr(action.Name),
		Type:        lo.ToPtr(action.Type),
		ConfigClass: action.ConfigClass,
		Tags:        types.JSONStringMap(action.Tags),
	}

	if scraperID != "" {
		item.ScraperID = lo.ToPtr(scraperID)
	}

	if action.Config != "" {
		item.Config = lo.ToPtr(action.Config)
	}
	if action.Health != "" {
		h := models.Health(action.Health)
		item.Health = &h
	}
	if action.Status != "" {
		item.Status = lo.ToPtr(action.Status)
	}
	if len(action.Labels) > 0 {
		labels := types.JSONStringMap(action.Labels)
		item.Labels = &labels
	}

	if err := ctx.DB().Create(&item).Error; err != nil {
		return nil, fmt.Errorf("failed to create config item: %w", err)
	}

	return &CatalogResult{
		ID:          item.ID,
		Name:        action.Name,
		Type:        action.Type,
		ConfigClass: action.ConfigClass,
		Config:      action.Config,
		Health:      action.Health,
		Status:      action.Status,
		Tags:        action.Tags,
		Labels:      action.Labels,
		ScraperID:   scraperID,
	}, nil
}

// resolveScraperID looks up a scraper by UUID or namespace/name and returns its ID string.
func resolveScraperID(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}

	if _, err := uuid.Parse(ref); err == nil {
		var scraper models.ConfigScraper
		if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", ref).First(&scraper).Error; err != nil {
			return "", fmt.Errorf("scraper with id %q not found: %w", ref, err)
		}
		return scraper.ID.String(), nil
	}

	// Try namespace/name format
	var scraper models.ConfigScraper
	query := ctx.DB().Where("deleted_at IS NULL")

	parts := splitNamespaceName(ref)
	if len(parts) == 2 {
		query = query.Where("namespace = ? AND name = ?", parts[0], parts[1])
	} else {
		query = query.Where("name = ?", ref)
	}

	if err := query.First(&scraper).Error; err != nil {
		return "", fmt.Errorf("scraper %q not found: %w", ref, err)
	}

	return scraper.ID.String(), nil
}

func splitNamespaceName(ref string) []string {
	for i, c := range ref {
		if c == '/' {
			return []string{ref[:i], ref[i+1:]}
		}
	}
	return []string{ref}
}
