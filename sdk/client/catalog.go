package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/incident-commander/clientapi"
	"golang.org/x/sync/errgroup"
)

type CatalogRelationships = clientapi.CatalogRelationships
type CatalogChangeConfig = clientapi.CatalogChangeConfig
type CatalogChangeDetail = clientapi.CatalogChangeDetail
type CatalogInsightDetail = clientapi.CatalogInsightDetail
type CatalogInsightEvidence = clientapi.CatalogInsightEvidence
type CatalogInsightHypothesis = clientapi.CatalogInsightHypothesis
type CatalogInsightIncident = clientapi.CatalogInsightIncident

// catalogItemResponse bridges PostgREST's native JSONB config to ConfigItem's JSON string field.
type catalogItemResponse struct {
	clientapi.ConfigItem
	Config json.RawMessage `json:"config"`
}

const catalogChangeDetailSelect = "id,config_id,change_type,created_at,external_created_by,source,diff,details,patches,created_by,config:configs(id,name,type,config_class),artifacts:artifacts(*)::jsonb"
const catalogInsightDetailSelect = "id,config_id,scraper_id,analyzer,message,summary,status,severity,analysis_type,analysis,properties,source,first_observed,last_observed,is_pushed,config:configs(id,name,type,config_class)"
const catalogInsightSearchDetailSelect = catalogInsightDetailSelect + ",evidences(hypothesis:hypotheses(incident:incidents(incident_id)))"
const catalogItemBatchSize = 100
const catalogItemBatchConcurrency = 4
const catalogInsightBatchSize = 100
const catalogInsightBatchConcurrency = 4

// SearchCatalog runs a resource search against the remote server
// (POST /resources/search).
func (c *Client) SearchCatalog(ctx context.Context, req clientapi.SearchResourcesRequest) (*clientapi.SearchResourcesResponse, error) {
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
	var out clientapi.SearchResourcesResponse
	if err := decodeJSON(r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchCatalogChanges returns config-scoped catalog changes, including changes
// reached through the requested relationship direction.
func (c *Client) SearchCatalogChanges(ctx context.Context, req clientapi.CatalogChangesSearchRequest) (*clientapi.CatalogChangesSearchResponse, error) {
	r, err := c.R(ctx).Post(c.apiPath("/catalog/changes"), req)
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
	var out clientapi.CatalogChangesSearchResponse
	if err := decodeJSON(r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCatalogItem fetches a single catalog item by id (GET /resources/:id).
func (c *Client) GetCatalogItem(ctx context.Context, id string) (*clientapi.ConfigItem, error) {
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
	var out clientapi.ConfigItem
	if err := decodeJSON(r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCatalogItemSummary fetches a catalog item's computed summary fields.
func (c *Client) GetCatalogItemSummary(ctx context.Context, id string) (*clientapi.ConfigItemSummary, error) {
	r, err := c.R(ctx).
		QueryParam("id", "eq."+id).
		QueryParam("select", "*").
		Get(c.apiPath("/db/configs"))
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

	var out []clientapi.ConfigItemSummary
	if err := decodeJSON(r, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return &out[0], nil
}

// GetCatalogItems fetches available catalog items in bounded batches, preserves
// requested order, and omits IDs that are no longer visible during hydration.
func (c *Client) GetCatalogItems(ctx context.Context, ids []string) ([]clientapi.ConfigItem, error) {
	if len(ids) == 0 {
		return []clientapi.ConfigItem{}, nil
	}

	batchCount := (len(ids) + catalogItemBatchSize - 1) / catalogItemBatchSize
	batches := make([][]clientapi.ConfigItem, batchCount)
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(catalogItemBatchConcurrency)

	for batchIndex := range batchCount {
		start := batchIndex * catalogItemBatchSize
		end := min(start+catalogItemBatchSize, len(ids))
		group.Go(func() error {
			batch, err := c.getCatalogItemsBatch(groupCtx, ids[start:end])
			if err != nil {
				return fmt.Errorf("catalog items batch %d: %w", batchIndex+1, err)
			}
			batches[batchIndex] = batch
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, err
	}

	itemsByID := make(map[string]clientapi.ConfigItem, len(ids))
	for _, batch := range batches {
		for _, item := range batch {
			itemsByID[item.ID.String()] = item
		}
	}

	result := make([]clientapi.ConfigItem, 0, len(ids))
	for _, id := range ids {
		item, ok := itemsByID[id]
		if ok {
			result = append(result, item)
		}
	}
	return result, nil
}

func (c *Client) getCatalogItemsBatch(ctx context.Context, ids []string) ([]clientapi.ConfigItem, error) {
	r, err := c.R(ctx).
		QueryParam("id", "in.("+strings.Join(ids, ",")+")").
		QueryParam("select", "*").
		Get(c.apiPath("/db/config_items"))
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

	var response []catalogItemResponse
	if err := decodeJSON(r, &response); err != nil {
		return nil, err
	}

	out := make([]clientapi.ConfigItem, 0, len(response))
	for _, raw := range response {
		item := raw.ConfigItem
		rawConfig := strings.TrimSpace(string(raw.Config))
		if rawConfig != "" && rawConfig != "null" {
			config := rawConfig
			if rawConfig[0] == '"' {
				if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
					return nil, err
				}
			}
			item.Config = &config
		}
		out = append(out, item)
	}
	return out, nil
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

// GetCatalogInsights fetches insight details in bounded, concurrent PostgREST batches.
func (c *Client) GetCatalogInsights(ctx context.Context, ids []string) ([]CatalogInsightDetail, error) {
	if len(ids) == 0 {
		return []CatalogInsightDetail{}, nil
	}

	batchCount := (len(ids) + catalogInsightBatchSize - 1) / catalogInsightBatchSize
	batches := make([][]CatalogInsightDetail, batchCount)
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(catalogInsightBatchConcurrency)

	for batchIndex := range batchCount {
		start := batchIndex * catalogInsightBatchSize
		end := min(start+catalogInsightBatchSize, len(ids))
		group.Go(func() error {
			batch, err := c.getCatalogInsightsBatch(groupCtx, ids[start:end])
			if err != nil {
				return fmt.Errorf("catalog insights batch %d: %w", batchIndex+1, err)
			}
			batches[batchIndex] = batch
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, err
	}

	result := make([]CatalogInsightDetail, 0, len(ids))
	for _, batch := range batches {
		result = append(result, batch...)
	}
	return result, nil
}

func (c *Client) getCatalogInsightsBatch(ctx context.Context, ids []string) ([]CatalogInsightDetail, error) {
	r, err := c.R(ctx).
		QueryParam("id", "in.("+strings.Join(ids, ",")+")").
		QueryParam("select", catalogInsightSearchDetailSelect).
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
	return out, nil
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
