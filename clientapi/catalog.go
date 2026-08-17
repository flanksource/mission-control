package clientapi

import (
	"time"

	"github.com/google/uuid"
)

const MaxSearchResourcesLimit = 1000

type ResourceSelector struct {
	Agent          string   `json:"agent,omitempty" yaml:"agent,omitempty"`
	Scope          string   `json:"scope,omitempty" yaml:"scope,omitempty"`
	Cache          string   `json:"cache,omitempty" yaml:"cache,omitempty"`
	Search         string   `json:"search,omitempty" yaml:"search,omitempty"`
	Limit          int      `json:"limit,omitempty" yaml:"limit,omitempty"`
	IncludeDeleted bool     `json:"includeDeleted,omitempty" yaml:"includeDeleted,omitempty"`
	ID             string   `json:"id,omitempty" yaml:"id,omitempty"`
	Name           string   `json:"name,omitempty" yaml:"name,omitempty"`
	Namespace      string   `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	TagSelector    string   `json:"tagSelector,omitempty" yaml:"tagSelector,omitempty"`
	LabelSelector  string   `json:"labelSelector,omitempty" yaml:"labelSelector,omitempty"`
	FieldSelector  string   `json:"fieldSelector,omitempty" yaml:"fieldSelector,omitempty"`
	Health         string   `json:"health,omitempty" yaml:"health,omitempty"`
	Types          []string `json:"types,omitempty" yaml:"types,omitempty"`
	Statuses       []string `json:"statuses,omitempty" yaml:"statuses,omitempty"`
}

type SearchResourcesRequest struct {
	Limit          int                `json:"limit"`
	Timestamps     bool               `json:"timestamps,omitempty"`
	Canaries       []ResourceSelector `json:"canaries"`
	Checks         []ResourceSelector `json:"checks"`
	Components     []ResourceSelector `json:"components"`
	Configs        []ResourceSelector `json:"configs"`
	ConfigChanges  []ResourceSelector `json:"config_changes"`
	ConfigAnalysis []ResourceSelector `json:"config_analysis"`
	Playbooks      []ResourceSelector `json:"playbooks"`
	Connections    []ResourceSelector `json:"connections"`
}

type SearchResourcesResponse struct {
	Canaries       []SelectedResource `json:"canaries,omitempty"`
	Checks         []SelectedResource `json:"checks,omitempty"`
	Components     []SelectedResource `json:"components,omitempty"`
	Configs        []SelectedResource `json:"configs,omitempty"`
	ConfigChanges  []SelectedResource `json:"config_changes,omitempty"`
	ConfigAnalysis []SelectedResource `json:"config_analysis,omitempty"`
	Playbooks      []SelectedResource `json:"playbooks,omitempty"`
	Connections    []SelectedResource `json:"connections,omitempty"`
}

type SelectedResource struct {
	ID        string            `json:"id"`
	Agent     string            `json:"agent"`
	Icon      string            `json:"icon,omitempty"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Type      string            `json:"type"`
	Tags      map[string]string `json:"tags,omitempty"`
	CreatedAt *time.Time        `json:"created_at,omitempty"`
	UpdatedAt *time.Time        `json:"updated_at,omitempty"`
	DeletedAt *time.Time        `json:"deleted_at,omitempty"`
	Health    string            `json:"health,omitempty"`
	Status    string            `json:"status,omitempty"`
	Severity  *string           `json:"severity,omitempty"`
}

type ConfigItem struct {
	ID            uuid.UUID          `json:"id"`
	ScraperID     *string            `json:"scraper_id,omitempty"`
	AgentID       uuid.UUID          `json:"agent_id,omitempty"`
	ConfigClass   string             `json:"config_class"`
	ExternalID    []string           `json:"external_id,omitempty"`
	Type          *string            `json:"type"`
	Status        *string            `json:"status"`
	Ready         bool               `json:"ready"`
	Health        *string            `json:"health"`
	Name          *string            `json:"name,omitempty"`
	Description   *string            `json:"description"`
	Config        *string            `json:"config"`
	Source        *string            `json:"source,omitempty"`
	ParentID      *uuid.UUID         `json:"parent_id,omitempty"`
	Path          string             `json:"path,omitempty"`
	CostPerMinute float64            `json:"cost_per_minute,omitempty"`
	CostTotal1d   float64            `json:"cost_total_1d,omitempty"`
	CostTotal7d   float64            `json:"cost_total_7d,omitempty"`
	CostTotal30d  float64            `json:"cost_total_30d,omitempty"`
	Labels        *map[string]string `json:"labels,omitempty"`
	Tags          map[string]string  `json:"tags,omitempty"`
	Properties    *CatalogProperties `json:"properties,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	InsertedAt    time.Time          `json:"inserted_at"`
	UpdatedAt     *time.Time         `json:"updated_at"`
	DeletedAt     *time.Time         `json:"deleted_at,omitempty"`
	DeleteReason  string             `json:"delete_reason,omitempty"`
}

type ConfigItemSummary struct {
	ID              uuid.UUID          `json:"id"`
	ScraperID       *string            `json:"scraper_id,omitempty"`
	ConfigClass     string             `json:"config_class"`
	ExternalID      []string           `json:"external_id,omitempty"`
	Type            *string            `json:"type"`
	Name            *string            `json:"name,omitempty"`
	Namespace       *string            `json:"namespace,omitempty"`
	Description     *string            `json:"description"`
	Source          *string            `json:"source,omitempty"`
	Labels          *map[string]string `json:"labels,omitempty"`
	Tags            map[string]string  `json:"tags,omitempty"`
	CreatedBy       *uuid.UUID         `json:"created_by,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       *time.Time         `json:"updated_at"`
	DeletedAt       *time.Time         `json:"deleted_at,omitempty"`
	CostPerMinute   *float64           `json:"cost_per_minute,omitempty"`
	CostTotal1h     *float64           `json:"cost_total_1h,omitempty"`
	CostTotal1d     *float64           `json:"cost_total_1d,omitempty"`
	CostTotal30d    *float64           `json:"cost_total_30d,omitempty"`
	BillingCurrency *string            `json:"billing_currency,omitempty"`
	MixedCurrency   bool               `json:"mixed_currency"`
	AgentID         uuid.UUID          `json:"agent_id,omitempty"`
	Status          *string            `json:"status"`
	Health          *string            `json:"health"`
	Ready           bool               `json:"ready"`
	Path            string             `json:"path,omitempty"`
	Changes         int                `json:"changes,omitempty"`
	Analysis        *map[string]any     `json:"analysis,omitempty"`
}

type CatalogText struct {
	Tooltip string `json:"tooltip,omitempty"`
	Icon    string `json:"icon,omitempty"`
	Text    string `json:"text,omitempty"`
	Label   string `json:"label,omitempty"`
}

type CatalogLink struct {
	Type        string `json:"type,omitempty"`
	URL         string `json:"url,omitempty"`
	CatalogText `json:",inline"`
}

type CatalogProperty struct {
	Type           string        `json:"type,omitempty"`
	Label          string        `json:"label,omitempty"`
	Name           string        `json:"name,omitempty"`
	Tooltip        string        `json:"tooltip,omitempty"`
	Icon           string        `json:"icon,omitempty"`
	Color          string        `json:"color,omitempty"`
	Order          int           `json:"order,omitempty"`
	Headline       bool          `json:"headline,omitempty"`
	Hidden         bool          `json:"hidden,omitempty"`
	Text           string        `json:"text,omitempty"`
	Value          *int64        `json:"value,omitempty"`
	Unit           string        `json:"unit,omitempty"`
	Max            *int64        `json:"max,omitempty"`
	Min            *int64        `json:"min,omitempty"`
	Status         string        `json:"status,omitempty"`
	LastTransition string        `json:"lastTransition,omitempty"`
	Links          []CatalogLink `json:"links,omitempty"`
}

type CatalogProperties []*CatalogProperty

type ConfigTreeNode struct {
	ConfigItem `json:",inline"`
	EdgeType   string            `json:"edgeType,omitempty"`
	Relation   string            `json:"relation,omitempty"`
	Children   []*ConfigTreeNode `json:"children,omitempty"`
}

type CatalogRelationships struct {
	ID       uuid.UUID       `json:"id"`
	Incoming *ConfigTreeNode `json:"incoming"`
	Outgoing *ConfigTreeNode `json:"outgoing"`
}

type ChangeRelationDirection string

const (
	CatalogChangeRecursiveUpstream   ChangeRelationDirection = "upstream"
	CatalogChangeRecursiveDownstream ChangeRelationDirection = "downstream"
	CatalogChangeRecursiveNone       ChangeRelationDirection = "none"
	CatalogChangeRecursiveAll        ChangeRelationDirection = "all"
)

type BaseCatalogSearch struct {
	CatalogID             string                  `json:"id"`
	ConfigType            string                  `json:"config_type"`
	IncludeDeletedConfigs bool                    `json:"include_deleted_configs"`
	Depth                 int                     `json:"depth"`
	Tags                  string                  `json:"tags"`
	AgentID               string                  `json:"agent_id"`
	From                  string                  `json:"from"`
	To                    string                  `json:"to"`
	PageSize              int                     `json:"page_size"`
	Page                  int                     `json:"page"`
	SortBy                string                  `json:"sort_by"`
	Recursive             ChangeRelationDirection `json:"recursive"`
	Soft                  bool                    `json:"soft"`
	Lenient               bool                    `json:"lenient"`
}

type CatalogChangesSearchRequest struct {
	BaseCatalogSearch `json:",inline"`
	ChangeType        string `json:"type"`
	Severity          string `json:"severity"`
	CreatedByRaw      string `json:"created_by"`
	Summary           string `json:"summary"`
	Source            string `json:"source"`
	FromInsertedAt    string `json:"from_inserted_at"`
	ToInsertedAt      string `json:"to_inserted_at"`
}

type ConfigChangeRow struct {
	AgentID           string            `json:"agent_id"`
	ExternalChangeID  string            `json:"external_change_id"`
	ID                string            `json:"id"`
	ConfigID          string            `json:"config_id"`
	DeletedAt         *time.Time        `json:"deleted_at,omitempty"`
	ChangeType        string            `json:"change_type"`
	Severity          string            `json:"severity"`
	Source            string            `json:"source"`
	Summary           string            `json:"summary,omitempty"`
	CreatedAt         *time.Time        `json:"created_at"`
	Count             int               `json:"count"`
	FirstObserved     *time.Time        `json:"first_observed,omitempty"`
	ConfigName        string            `json:"name,omitempty"`
	ConfigType        string            `json:"type,omitempty"`
	Tags              map[string]string `json:"tags,omitempty"`
	CreatedBy         *uuid.UUID        `json:"created_by,omitempty"`
	ExternalCreatedBy string            `json:"external_created_by,omitempty"`
	Path              string            `json:"path,omitempty"`
	InsertedAt        *time.Time        `json:"inserted_at,omitempty"`
}

type CatalogChangesSearchResponse struct {
	Summary map[string]int    `json:"summary,omitempty"`
	Total   int64             `json:"total,omitempty"`
	Changes []ConfigChangeRow `json:"changes,omitempty"`
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

type CatalogInsightDetail struct {
	ID            uuid.UUID                `json:"id"`
	ConfigID      uuid.UUID                `json:"config_id"`
	ScraperID     *uuid.UUID               `json:"scraper_id,omitempty"`
	Analyzer      string                   `json:"analyzer"`
	Message       string                   `json:"message,omitempty"`
	Summary       string                   `json:"summary,omitempty"`
	Status        string                   `json:"status,omitempty"`
	Severity      string                   `json:"severity,omitempty"`
	AnalysisType  string                   `json:"analysis_type,omitempty"`
	Analysis      map[string]any           `json:"analysis,omitempty"`
	Properties    *CatalogProperties       `json:"properties,omitempty"`
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
