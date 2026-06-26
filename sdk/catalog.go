package sdk

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
)

// CatalogRelationships mirrors the server's catalog relationships response
// (catalog.ConfigRelationshipsResponse) without importing the heavy catalog
// package. Both directions are config trees rooted at ID.
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

type CatalogSearchResource struct {
	ID        string            `json:"id"`
	Agent     string            `json:"agent"`
	Icon      string            `json:"icon,omitempty"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Type      string            `json:"type"`
	Tags      map[string]string `json:"tags,omitempty"`
	Health    string            `json:"health,omitempty"`
	Status    string            `json:"status,omitempty"`
	Severity  *string           `json:"severity,omitempty"`
}

type CatalogInsightSearchRequest struct {
	Limit          int                      `json:"limit"`
	ConfigAnalysis []types.ResourceSelector `json:"config_analysis"`
}

type CatalogInsightSearchResponse struct {
	ConfigAnalysis []CatalogSearchResource `json:"config_analysis,omitempty"`
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

type CatalogInsightDetail struct {
	ID            uuid.UUID            `json:"id"`
	ConfigID      uuid.UUID            `json:"config_id"`
	ScraperID     *uuid.UUID           `json:"scraper_id,omitempty"`
	Analyzer      string               `json:"analyzer"`
	Message       string               `json:"message,omitempty"`
	Summary       string               `json:"summary,omitempty"`
	Status        string               `json:"status,omitempty"`
	Severity      models.Severity      `json:"severity,omitempty"`
	AnalysisType  models.AnalysisType  `json:"analysis_type,omitempty"`
	Analysis      types.JSONMap        `json:"analysis,omitempty"`
	Properties    *types.Properties    `json:"properties,omitempty"`
	Source        string               `json:"source,omitempty"`
	FirstObserved *time.Time           `json:"first_observed,omitempty"`
	LastObserved  *time.Time           `json:"last_observed,omitempty"`
	IsPushed      bool                 `json:"is_pushed,omitempty"`
	Config        *CatalogChangeConfig `json:"config,omitempty"`
}

const catalogChangeDetailSelect = "id,config_id,change_type,created_at,external_created_by,source,diff,details,patches,created_by,config:configs(id,name,type,config_class),artifacts:artifacts(*)::jsonb"
const catalogInsightDetailSelect = "id,config_id,scraper_id,analyzer,message,summary,status,severity,analysis_type,analysis,properties,source,first_observed,last_observed,is_pushed,config:configs(id,name,type,config_class)"

// SearchCatalog runs a resource search against the remote server
// (POST /resources/search).
func (c *Client) SearchCatalog(ctx context.Context, req query.SearchResourcesRequest) (*query.SearchResourcesResponse, error) {
	r, err := c.R(ctx).Post(c.apiPath("/resources/search"), req)
	if err != nil {
		return nil, err
	}
	if !r.IsOK() {
		body, _ := r.AsString()
		if looksLikeHTML(r.Header.Get("Content-Type"), body) {
			return nil, ErrHTMLResponse
		}
		return nil, fmt.Errorf("server returned %d: %s", r.StatusCode, strings.TrimSpace(body))
	}
	var out query.SearchResourcesResponse
	if err := decodeJSON(r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchCatalogInsights runs a config insight search against POST /resources/search.
func (c *Client) SearchCatalogInsights(ctx context.Context, req CatalogInsightSearchRequest) (*CatalogInsightSearchResponse, error) {
	r, err := c.R(ctx).Post(c.apiPath("/resources/search"), req)
	if err != nil {
		return nil, err
	}
	if !r.IsOK() {
		body, _ := r.AsString()
		if looksLikeHTML(r.Header.Get("Content-Type"), body) {
			return nil, ErrHTMLResponse
		}
		return nil, fmt.Errorf("server returned %d: %s", r.StatusCode, strings.TrimSpace(body))
	}
	var out CatalogInsightSearchResponse
	if err := decodeJSON(r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCatalogItem fetches a single catalog item by id (GET /resources/:id).
func (c *Client) GetCatalogItem(ctx context.Context, id string) (*models.ConfigItem, error) {
	r, err := c.R(ctx).Get(c.apiPath("/resources/" + id))
	if err != nil {
		return nil, err
	}
	if !r.IsOK() {
		body, _ := r.AsString()
		if looksLikeHTML(r.Header.Get("Content-Type"), body) {
			return nil, ErrHTMLResponse
		}
		return nil, fmt.Errorf("server returned %d: %s", r.StatusCode, strings.TrimSpace(body))
	}
	var out models.ConfigItem
	if err := decodeJSON(r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCatalogChange fetches full details for a catalog change from PostgREST.
func (c *Client) GetCatalogChange(ctx context.Context, id string) (*CatalogChangeDetail, error) {
	r, err := c.R(ctx).
		QueryParam("id", "eq."+id).
		QueryParam("select", catalogChangeDetailSelect).
		Get(c.apiPath("/db/config_changes"))
	if err != nil {
		return nil, err
	}
	if !r.IsOK() {
		body, _ := r.AsString()
		if looksLikeHTML(r.Header.Get("Content-Type"), body) {
			return nil, ErrHTMLResponse
		}
		return nil, fmt.Errorf("server returned %d: %s", r.StatusCode, strings.TrimSpace(body))
	}
	var out []CatalogChangeDetail
	if err := decodeJSON(r, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return &out[0], nil
}

// GetCatalogInsight fetches full details for a catalog insight from PostgREST.
func (c *Client) GetCatalogInsight(ctx context.Context, id string) (*CatalogInsightDetail, error) {
	r, err := c.R(ctx).
		QueryParam("id", "eq."+id).
		QueryParam("select", catalogInsightDetailSelect).
		Get(c.apiPath("/db/config_analysis"))
	if err != nil {
		return nil, err
	}
	if !r.IsOK() {
		body, _ := r.AsString()
		if looksLikeHTML(r.Header.Get("Content-Type"), body) {
			return nil, ErrHTMLResponse
		}
		return nil, fmt.Errorf("server returned %d: %s", r.StatusCode, strings.TrimSpace(body))
	}
	var out []CatalogInsightDetail
	if err := decodeJSON(r, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return &out[0], nil
}

// GetCatalogRelationships fetches the incoming/outgoing config tree for a
// catalog item (GET /catalog/:id/relationships).
func (c *Client) GetCatalogRelationships(ctx context.Context, id string) (*CatalogRelationships, error) {
	r, err := c.R(ctx).Get(c.apiPath("/catalog/" + id + "/relationships"))
	if err != nil {
		return nil, err
	}
	if !r.IsOK() {
		body, _ := r.AsString()
		if looksLikeHTML(r.Header.Get("Content-Type"), body) {
			return nil, ErrHTMLResponse
		}
		return nil, fmt.Errorf("server returned %d: %s", r.StatusCode, strings.TrimSpace(body))
	}
	var out CatalogRelationships
	if err := decodeJSON(r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
