package context

import (
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
)

// Context carries the entire knowledge.
type Context struct {
	Configs []Config `json:"configs"`

	// Edges holds the relationship graph
	Edges []Edge `json:"edges"`
}

// Edge represents a connection between two nodes in the graph.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Analysis represents an analysis performed on a configuration.
type Analysis struct {
	ID            uuid.UUID     `json:"id"`
	Analyzer      string        `json:"analyzer"`
	Message       string        `json:"message"`
	Summary       string        `json:"summary"`
	Status        string        `json:"status"`
	Severity      string        `json:"severity"`
	AnalysisType  string        `json:"analysis_type"`
	Analysis      types.JSONMap `json:"analysis"`
	Source        string        `json:"source"`
	FirstObserved *time.Time    `json:"first_observed"`
	LastObserved  *time.Time    `json:"last_observed"`
}

// FromModel populates the Analysis struct from a ConfigAnalysis model.
func (t *Analysis) FromModel(c models.ConfigAnalysis) {
	t.ID = c.ID
	t.Analyzer = c.Analyzer
	t.Message = c.Message
	t.Summary = c.Summary
	t.Status = c.Status
	t.Severity = string(c.Severity)
	t.AnalysisType = string(c.AnalysisType)
	t.Analysis = c.Analysis
	t.Source = c.Source
	t.FirstObserved = c.FirstObserved
	t.LastObserved = c.LastObserved
}

// Change represents a change made to a configuration.
type Change struct {
	Count         int        `json:"count"`
	CreatedBy     string     `json:"created_by"`
	FirstObserved *time.Time `json:"first_observed"`
	LastObserved  *time.Time `json:"last_observed"`
	Summary       string     `json:"summary"`
	Type          string     `json:"type"`
	Source        string     `json:"source"`

	// Note: Diff and Patches fields are omitted as they are not available
	// in related_changes_recursive queries.
}

// FromModel populates the Change struct from a ConfigChangeRow model.
func (t *Change) FromModel(c query.ConfigChangeRow) {
	t.Count = c.Count
	t.CreatedBy = c.ExternalCreatedBy
	t.FirstObserved = c.FirstObserved
	t.LastObserved = c.CreatedAt
	t.Source = c.Source
	t.Summary = c.Summary
	t.Type = c.ChangeType
}

// Config represents a configuration in the knowledge graph.
type Config struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Config      any            `json:"config,omitempty"`
	Health      *models.Health `json:"health,omitempty"`
	Status      *string        `json:"status,omitempty"`
	Description *string        `json:"description,omitempty"`
	ID          string         `json:"id"`
	Updated     *time.Time     `json:"updated,omitempty"`
	Deleted     *time.Time     `json:"deleted,omitempty"`
	Created     time.Time      `json:"created"`
	Changes     []Change       `json:"changes,omitempty"`
	Analyses    []Analysis     `json:"analyses,omitempty"`

	// Don't send these to the LLM
	Labels *types.JSONStringMap `json:"-"`
	Tags   map[string]string    `json:"-"`
}

func (t *Config) GetTrimmedLabels() []models.Label {
	cc := models.ConfigItem{Labels: t.Labels, Tags: t.Tags}
	return cc.GetTrimmedLabels()
}
