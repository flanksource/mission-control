package actions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/template"
	"time"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/commons/duration"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/shutdown"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/llm"
)

var recommendPlaybookPrompt *template.Template

func init() {
	var err error

	recommendPlaybookPrompt, err = template.New("recommend").Parse(`
	<background>
	Playbooks automate common workflows and processes by defining reusable templates of actions that can be triggered on 
	a resource. Playbooks take parameters as defined in the parameters section of the playbook.
	</background>

	Analyze a list of playbooks given below and find the most suitable ones to tackle the issue described in the diagnosis report.
	It's important to select the most relevant playbooks that are most likely to resolve the issue.

	<playbooks>
	{{.playbooks}}
	</playbooks>`)
	if err != nil {
		shutdown.ShutdownAndExit(1, "bad template for playbook recommendation prompt")
	}
}

// AIAction represents an action that uses AI to analyze configurations and recommend playbooks.
type AIAction struct {
	PlaybookID uuid.UUID // ID of the playbook that is executing this action
}

// AIActionResult contains the results of an AI action execution.
type AIActionResult struct {
	Markdown             string `json:"markdown,omitempty"`             // Markdown formatted diagnosis report
	Slack                string `json:"slack,omitempty"`                // Slack blocks formatted diagnosis report
	RecommendedPlaybooks string `json:"recommendedPlaybooks,omitempty"` // Recommended playbooks in Slack blocks format

	// Prompt can get very large so we don't want to store it in the database.
	// It's stored as an artifact instead.
	Prompt string `json:"-"`
}

func (t *AIActionResult) GetArtifacts() []artifacts.Artifact {
	return []artifacts.Artifact{
		{
			ContentType: "markdown",
			Content:     io.NopCloser(strings.NewReader(t.Prompt)),
			Path:        "prompt.md",
		},
	}
}

// Run executes the AI action with the given specification.
// It builds a prompt using the knowledge graph, sends it to the LLM,
// and processes the response into the requested formats.
func (t *AIAction) Run(ctx context.Context, spec v1.AIAction) (*AIActionResult, error) {
	var result AIActionResult
	if spec.Backend == "" {
		spec.Backend = api.LLMBackendOpenAI
	}

	knowledgebase, prompt, err := buildPrompt(ctx, spec.Prompt, spec.AIActionContext)
	if err != nil {
		return nil, fmt.Errorf("failed to form prompt: %w", err)
	}
	result.Prompt = strings.Join(prompt, "\n")

	if err := spec.AIActionClient.Populate(ctx); err != nil {
		return nil, fmt.Errorf("failed to populate llm client connection: %w", err)
	}

	if spec.DryRun {
		return &AIActionResult{Markdown: strings.Join(prompt, "\n")}, nil
	}

	llmConf := llm.Config{AIActionClient: spec.AIActionClient, ResponseFormat: llm.ResponseFormatDiagnosis}
	response, conversation, err := llm.Prompt(ctx, llmConf, spec.SystemPrompt, prompt...)
	if err != nil {
		return nil, err
	}
	result.Markdown = response

	var diagnosisReport llm.DiagnosisReport
	if err := json.Unmarshal([]byte(response), &diagnosisReport); err != nil {
		return nil, fmt.Errorf("failed to unmarshal diagnosis report: %w", err)
	}

	for _, format := range lo.Uniq(spec.Formats) {
		switch format {
		case v1.AIActionFormatSlack:
			result.Slack, err = slackBlocks(knowledgebase, diagnosisReport, llm.PlaybookRecommendations{})
			if err != nil {
				return nil, fmt.Errorf("failed to merge blocks: %w", err)
			}

		case v1.AIActionFormatRecommendPlaybook:
			config, err := query.GetCachedConfig(ctx, spec.Config)
			if err != nil {
				return nil, err
			} else if config == nil {
				return nil, errors.New("config not found")
			}

			_, supportedPlaybooks, err := db.FindPlaybooksForConfig(ctx, *config)
			if err != nil {
				return nil, err
			}

			// The playbook shouldn't recommend itself.
			supportedPlaybooks = lo.Filter(supportedPlaybooks, func(c *models.Playbook, _ int) bool {
				return c.ID != t.PlaybookID
			})

			// Only provide those playbooks that match the given filter
			if len(spec.RecommendPlaybooks) > 0 {
				supportedPlaybooks = types.MatchSelectables(supportedPlaybooks, spec.RecommendPlaybooks...)
			}

			playbooksJSON, err := json.MarshalIndent(supportedPlaybooks, "", "\t")
			if err != nil {
				return nil, err
			}

			var prompt bytes.Buffer
			if err := recommendPlaybookPrompt.Execute(&prompt, map[string]any{"playbooks": string(playbooksJSON)}); err != nil {
				return nil, err
			}

			llmConf.ResponseFormat = llm.ResponseFormatPlaybookRecommendations
			response, _, err := llm.PromptWithHistory(ctx, llmConf, conversation, prompt.String())
			if err != nil {
				return nil, fmt.Errorf("failed to generate playbook recommendation: %w", err)
			}

			var recommendations llm.PlaybookRecommendations
			if err := json.Unmarshal([]byte(response), &recommendations); err != nil {
				return nil, fmt.Errorf("failed to unmarshal diagnosis report: %w", err)
			}

			blocks, err := slackBlocks(knowledgebase, diagnosisReport, recommendations)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal blocks: %w", err)
			}
			result.RecommendedPlaybooks = string(blocks)
		}
	}

	return &result, nil
}

// buildPrompt constructs a prompt for the LLM by combining the user prompt with knowledge graph data.
// It returns the knowledge graph, the complete prompt array, and any error encountered.
func buildPrompt(ctx context.Context, prompt string, spec v1.AIActionContext) (*KnowledgeGraph, []string, error) {
	knowledge, err := getKnowledgeGraph(ctx, spec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get prompt context: %w", err)
	}

	knowledgeJSON, err := json.MarshalIndent(knowledge, "", "\t")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get prompt context: %w", err)
	}

	output := []string{prompt, jsonBlock(string(knowledgeJSON))}
	if ctx.Properties().On(false, "playbook.action.ai.log-prompt") {
		fmt.Println(output)
	}

	return knowledge, output, nil
}

// jsonBlock wraps the given code in a JSON code block for markdown formatting.
func jsonBlock(code string) string {
	return fmt.Sprintf(jsonCodeBlockFormat, code)
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
	Name     string     `json:"name"`
	Type     string     `json:"type"`
	Config   string     `json:"config"`
	ID       string     `json:"id"`
	Updated  *time.Time `json:"updated"`
	Deleted  *time.Time `json:"deleted"`
	Created  time.Time  `json:"created"`
	Changes  []Change   `json:"changes"`
	Analyses []Analysis `json:"analyses"`

	// Don't send these to the LLM
	Labels *types.JSONStringMap `json:"-"`
	Tags   map[string]string    `json:"-"`
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

// AddAnalysis adds the given analyses to the knowledge graph.
// It converts each ConfigAnalysis to an Analysis and adds it to the appropriate Config.
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

// AddChanges adds the given changes to the knowledge graph.
// It converts each ConfigChangeRow to a Change and adds it to the appropriate Config.
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

// getKnowledgeGraph builds a knowledge graph for the given context.
// It fetches the main config and its relationships, changes, and analyses.
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
			Labels:  config.Labels,
			Tags:    config.Tags,
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

// processRelationship processes a relationship for the knowledge graph.
// It fetches related configs, adds them to the graph, and fetches their changes and analyses if requested.
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
