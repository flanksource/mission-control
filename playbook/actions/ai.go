package actions

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/commons/duration"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/llm"
)

type AIAction struct{}

type AIActionResult struct {
	Logs string `json:"logs,omitempty"` // TODO: only naming this "logs" because the frontend has proper formatted display for this field
}

func (t *AIAction) Run(ctx context.Context, spec v1.AIAction) (*AIActionResult, error) {
	if spec.Backend == "" {
		spec.Backend = api.LLMBackendOpenAI
	}

	if apiKey, err := ctx.GetEnvValueFromCache(spec.APIKey, ctx.GetNamespace()); err != nil {
		return nil, err
	} else {
		spec.APIKey.ValueStatic = apiKey
	}

	prompt, err := buildPrompt(ctx, spec.Prompt, spec.AIActionContext)
	if err != nil {
		return nil, fmt.Errorf("failed to form prompt: %w", err)
	}

	llmConf := llm.Config{AIActionClient: spec.AIActionClient, UseAgent: spec.UseAgent}
	response, err := llm.Prompt(ctx, llmConf, spec.SystemPrompt, prompt...)
	if err != nil {
		return nil, err
	}

	return &AIActionResult{Logs: response}, nil
}

func buildPrompt(ctx context.Context, prompt string, spec v1.AIActionContext) ([]string, error) {
	knowledge, err := getKnowledgeGraph(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt context: %w", err)
	}

	knowledgeJSON, err := json.Marshal(knowledge)
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt context: %w", err)
	}

	return []string{prompt, jsonBlock(string(knowledgeJSON))}, nil
}

func jsonBlock(code string) string {
	const format = "```json\n%s\n```"
	return fmt.Sprintf(format, code)
}

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

	// TODO: These are not present in the related_changes_recursive.
	// Diff    string `json:"diff,omitempty"`
	// Patches string `json:"patches,,omitempty"`
}

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
	Name     string     `json:"name"`
	Type     string     `json:"type"`
	Config   string     `json:"config"`
	ID       string     `json:"id"`
	Updated  *time.Time `json:"updated"`
	Deleted  *time.Time `json:"deleted"`
	Created  time.Time  `json:"created"`
	Changes  []Change   `json:"changes"`
	Analyses []Analysis `json:"analyses"`
}

// Edge represents a connection between two nodes in the graph.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// KnowledgeGraph represents the entire knowledge graph structure.
type KnowledgeGraph struct {
	Configs []Config `json:"configs"`
	Graph   []Edge   `json:"graph"`
}

func (t *KnowledgeGraph) AddAnalysis(analyses ...models.ConfigAnalysis) {
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

func (t *KnowledgeGraph) AddChanges(changes ...query.ConfigChangeRow) {
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

func getKnowledgeGraph(ctx context.Context, spec v1.AIActionContext) (*KnowledgeGraph, error) {
	var kg KnowledgeGraph

	config, err := query.GetCachedConfig(ctx, spec.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to get config (%s): %w", spec.Config, err)
	} else if config == nil {
		return nil, fmt.Errorf("config doesn't exist (%s)", spec.Config)
	} else {
		kg.Configs = append(kg.Configs, Config{
			ID:      config.ID.String(),
			Name:    lo.FromPtr(config.Name),
			Type:    lo.FromPtr(config.Type),
			Config:  lo.FromPtr(config.Config),
			Created: config.CreatedAt,
			Updated: config.UpdatedAt,
			Deleted: config.DeletedAt,
		})
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

	if spec.Analysis.Since != "" {
		analyses, err := getConfigAnalysis(ctx, config.ID.String(), spec.Analysis.Since)
		if err != nil {
			return nil, err
		}
		kg.AddAnalysis(analyses...)
	}

	return &kg, nil
}

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

func (t *KnowledgeGraph) processRelationship(ctx context.Context, configID uuid.UUID, relationship v1.AIActionRelationship) error {
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
		})

		for _, relatedID := range rc.RelatedIDs {
			if !lo.Contains(relatedConfigIDs, relatedID) {
				continue
			}

			t.Graph = append(t.Graph, Edge{
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
