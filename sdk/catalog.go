package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/clientapi"
)

type CatalogRelationships struct {
	ID       uuid.UUID             `json:"id"`
	Incoming *query.ConfigTreeNode `json:"incoming"`
	Outgoing *query.ConfigTreeNode `json:"outgoing"`
}

type CatalogChangeConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	ConfigClass string `json:"config_class"`
}

type CatalogChangeDetail struct {
	ID                string               `json:"id"`
	ConfigID          string               `json:"config_id"`
	ChangeType        string               `json:"change_type"`
	CreatedAt         *time.Time           `json:"created_at,omitempty"`
	ExternalCreatedBy *string              `json:"external_created_by,omitempty"`
	Source            *string              `json:"source,omitempty"`
	Diff              *string              `json:"diff,omitempty"`
	Details           map[string]any       `json:"details,omitempty"`
	Patches           any                  `json:"patches,omitempty"`
	CreatedBy         *uuid.UUID           `json:"created_by,omitempty"`
	Config            *CatalogChangeConfig `json:"config,omitempty"`
	Artifacts         []map[string]any     `json:"artifacts,omitempty"`
}

// CatalogChange is a config-scoped row from catalog_changes.
type CatalogChange struct {
	ID                string         `json:"id"`
	ConfigID          string         `json:"config_id"`
	Name              *string        `json:"name,omitempty"`
	Type              *string        `json:"type,omitempty"`
	ConfigClass       *string        `json:"config_class,omitempty"`
	ChangeType        string         `json:"change_type"`
	Severity          string         `json:"severity,omitempty"`
	Source            string         `json:"source,omitempty"`
	Summary           string         `json:"summary,omitempty"`
	CreatedAt         *time.Time     `json:"created_at,omitempty"`
	ExternalCreatedBy *string        `json:"external_created_by,omitempty"`
	Details           map[string]any `json:"details,omitempty"`
	Patches           any            `json:"patches,omitempty"`
	Diff              *string        `json:"diff,omitempty"`
	Count             int            `json:"count,omitempty"`
}

type CatalogChangeOptions struct {
	ChangeTypes []string
	Sources     []string
	Since       *time.Time
	Limit       int
}

type CatalogInsightDetail struct {
	ID            uuid.UUID                `json:"id"`
	ConfigID      uuid.UUID                `json:"config_id"`
	ScraperID     *uuid.UUID               `json:"scraper_id,omitempty"`
	Analyzer      string                   `json:"analyzer"`
	Message       string                   `json:"message,omitempty"`
	Summary       string                   `json:"summary,omitempty"`
	Status        string                   `json:"status,omitempty"`
	Severity      models.Severity          `json:"severity,omitempty"`
	AnalysisType  models.AnalysisType      `json:"analysis_type,omitempty"`
	Analysis      types.JSONMap            `json:"analysis,omitempty"`
	Properties    *types.Properties        `json:"properties,omitempty"`
	Source        string                   `json:"source,omitempty"`
	FirstObserved *time.Time               `json:"first_observed,omitempty"`
	LastObserved  *time.Time               `json:"last_observed,omitempty"`
	IsPushed      bool                     `json:"is_pushed,omitempty"`
	Config        *CatalogChangeConfig     `json:"config,omitempty"`
	Evidences     []CatalogInsightEvidence `json:"evidences,omitempty"`
}

type CatalogInsightEvidence struct {
	Hypothesis *CatalogInsightHypothesis `json:"hypothesis,omitempty"`
}

type CatalogInsightHypothesis struct {
	Incident *CatalogInsightIncident `json:"incident,omitempty"`
}

type CatalogInsightIncident struct {
	IncidentID string `json:"incident_id,omitempty"`
}

// SearchCatalog runs a resource search against the remote server.
func (c *Client) SearchCatalog(ctx context.Context, request query.SearchResourcesRequest) (*query.SearchResourcesResponse, error) {
	var dto clientapi.SearchResourcesRequest
	if err := convertJSON(request, &dto); err != nil {
		return nil, err
	}
	response, err := c.leanClient().SearchCatalog(ctx, dto)
	if err != nil {
		return nil, err
	}
	var out query.SearchResourcesResponse
	if err := convertJSON(response, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchCatalogChanges returns config-scoped catalog changes.
func (c *Client) SearchCatalogChanges(ctx context.Context, request query.CatalogChangesSearchRequest) (*query.CatalogChangesSearchResponse, error) {
	var dto clientapi.CatalogChangesSearchRequest
	if err := convertJSON(request, &dto); err != nil {
		return nil, err
	}
	response, err := c.leanClient().SearchCatalogChanges(ctx, dto)
	if err != nil {
		return nil, err
	}
	var out query.CatalogChangesSearchResponse
	if err := convertJSON(response, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCatalogItem fetches a single catalog item by ID.
func (c *Client) GetCatalogItem(ctx context.Context, id string) (*models.ConfigItem, error) {
	item, err := c.leanClient().GetCatalogItem(ctx, id)
	if err != nil {
		return nil, err
	}
	var out models.ConfigItem
	if err := convertJSON(item, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCatalogItemSummary fetches a catalog item's computed summary fields.
func (c *Client) GetCatalogItemSummary(ctx context.Context, id string) (*models.ConfigItemSummary, error) {
	summary, err := c.leanClient().GetCatalogItemSummary(ctx, id)
	if err != nil {
		return nil, err
	}
	var out models.ConfigItemSummary
	if err := convertJSON(summary, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCatalogItems fetches catalog items while preserving requested order.
func (c *Client) GetCatalogItems(ctx context.Context, ids []string) ([]models.ConfigItem, error) {
	items, err := c.leanClient().GetCatalogItems(ctx, ids)
	if err != nil {
		return nil, err
	}
	var out []models.ConfigItem
	if err := convertJSON(items, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCatalogChange fetches full details for a catalog change.
func (c *Client) GetCatalogChange(ctx context.Context, id string) (*CatalogChangeDetail, error) {
	change, err := c.leanClient().GetCatalogChange(ctx, id)
	if err != nil {
		return nil, err
	}
	var out CatalogChangeDetail
	if err := convertJSON(change, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCatalogChanges returns config-scoped history from catalog_changes. The
// exact total lets projection callers fail rather than silently producing a
// partial audit history.
func (c *Client) ListCatalogChanges(ctx context.Context, opts CatalogChangeOptions) ([]CatalogChange, int, error) {
	params := url.Values{}
	params.Set("select", "*")
	params.Set("order", "created_at.desc")
	if len(opts.ChangeTypes) > 0 {
		params.Set("change_type", inList(opts.ChangeTypes))
	}
	if len(opts.Sources) > 0 {
		params.Set("source", inList(opts.Sources))
	}
	if opts.Since != nil {
		params.Set("created_at", "gte."+opts.Since.UTC().Format(time.RFC3339))
	}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}

	var out []CatalogChange
	total, err := c.pgGet(ctx, "catalog_changes", params, &out)
	return out, total, err
}

// GetCatalogInsight fetches full details for a catalog insight from PostgREST.
func (c *Client) GetCatalogInsight(ctx context.Context, id string) (*CatalogInsightDetail, error) {
	insight, err := c.leanClient().GetCatalogInsight(ctx, id)
	if err != nil {
		return nil, err
	}
	var out CatalogInsightDetail
	if err := convertJSON(insight, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCatalogInsights fetches insight details in bounded batches.
func (c *Client) GetCatalogInsights(ctx context.Context, ids []string) ([]CatalogInsightDetail, error) {
	insights, err := c.leanClient().GetCatalogInsights(ctx, ids)
	if err != nil {
		return nil, err
	}
	var out []CatalogInsightDetail
	if err := convertJSON(insights, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCatalogRelationships fetches incoming and outgoing catalog trees.
func (c *Client) GetCatalogRelationships(ctx context.Context, id string) (*CatalogRelationships, error) {
	relationships, err := c.leanClient().GetCatalogRelationships(ctx, id)
	if err != nil {
		return nil, err
	}
	var out CatalogRelationships
	if err := convertJSON(relationships, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
