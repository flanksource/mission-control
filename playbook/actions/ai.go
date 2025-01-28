package actions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

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

const slackPrompt = `
	Re-write the diagnosis formatted for a slack message.
	The output should be in pure json using Block Kit(https://api.slack.com/block-kit) - a UI framework for Slack apps.
	Example: output 
	{
		"blocks": [
			{
				"type": "section",
				"fields": [
					{
						"type": "mrkdwn",
						"text": "Statefulset: alertmanager"
					},
					{
						"type": "mrkdwn",
						"text": "*Namespace*: mc"
					},
					{
						"type": "mrkdwn",
						"text": "Deployment has pods that are in a crash loop."
					}
				]
			},
		]
	}
		
	Please do not add code blocks around the JSON output.`

func init() {
	var err error

	recommendPlaybookPrompt, err = template.New("recommend").Parse(`
	Analyze a list of playbooks and find the most suitable ones and create a Slack message using Block Kit, 
	allowing users to run these playbooks on a specific configuration.

	First, here's the list of playbooks you need to analyze:

	<playbooks>
	{{.playbooks}}
	</playbooks>

	Your goal is to create a Slack message that presents a summary of the diagnosis and the buttons for each applicable playbook. 
	Each button, when clicked, should trigger the execution of its corresponding playbook.

	Follow these steps to complete the task:

	- Create a concise summary of the diagnosis and wrap it in a slack text block.  

	- Analyze the given playbooks and identify which ones can be run on the current configuration.

	- For each applicable playbook, create a button element in the Slack Block Kit JSON structure.

	- Generate a URL for each button that will trigger the playbook execution. The URL should follow this format:
		GET {{.base_url}}/playbooks/runs
		With these query parameters:
		- playbook={playbook_id}
		- run=true (always set to true)
		- config_id={uuid_of_the_config}
		- params.{key}={url_encoded(value)}

		IMPORTANT: Ensure that you URL-encode the parameter values.

	- Compile all button elements into a single Block Kit JSON structure.

	- If no matching playbooks are found, create a message block with the text "No matching playbooks found."

	Here's an example of the expected output structure:

	{
		"blocks": [
			{
				"type": "section",
				"fields": [
					{
						"type": "mrkdwn",
						"text": "Deployment: grafana"
					},
					{
						"type": "mrkdwn",
						"text": "*Namespace*: mc"
					},
					{
						"type": "mrkdwn",
						"text": "Deployment has pods that are in a crash loop."
					}
				]
			},
			{
				"type": "actions",
				"block_id": "actionblock123",
				"elements": [
					{
						"type": "button",
						"text": {
							"type": "plain_text",
							"text": "Example Playbook"
						},
						"url": "{{.base_url}}/playbooks/runs?playbook=example-id&run=true&config_id=example-config-id&params.key=encoded_value"
					}
				]
			}
		]
	}

	Remember:
	- Ensure the JSON is valid and follows the Block Kit structure.
	- The response should contain nothing more than just the JSON.
		**DO NOT** wrap the json within a code block.
		Your next message should start with "{" and end with "}"
	- Present a concise summary of the diagnosis in a slack text block using mrkdwn.
	- Include all necessary query parameters in the URL.
	- URL-encode parameter values.
	- Use a unique block_id for each action block (e.g., "actionblock" followed by a random number).
	- If no playbooks match, return a block with a single text element.

	Now, analyze the playbooks and create the appropriate Slack message.`)
	if err != nil {
		shutdown.ShutdownAndExit(1, "bad template for playbook recommendation prompt")
	}
}

type AIAction struct {
	PlaybookID uuid.UUID
}

type AIActionResult struct {
	Markdown             string `json:"markdown,omitempty"`
	Slack                string `json:"slack,omitempty"`
	RecommendedPlaybooks string `json:"recommendedPlaybooks,omitempty"`
}

func (t *AIAction) Run(ctx context.Context, spec v1.AIAction) (*AIActionResult, error) {
	var result AIActionResult
	if spec.Backend == "" {
		spec.Backend = api.LLMBackendOpenAI
	}

	prompt, err := buildPrompt(ctx, spec.Prompt, spec.AIActionContext)
	if err != nil {
		return nil, fmt.Errorf("failed to form prompt: %w", err)
	}

	if err := spec.AIActionClient.Populate(ctx); err != nil {
		return nil, fmt.Errorf("faield to populate llm client connection: %w", err)
	}

	if spec.DryRun {
		return &AIActionResult{Markdown: strings.Join(prompt, "\n")}, nil
	}

	llmConf := llm.Config{AIActionClient: spec.AIActionClient}
	response, conversation, err := llm.Prompt(ctx, llmConf, spec.SystemPrompt, prompt...)
	if err != nil {
		return nil, err
	}
	result.Markdown = response

	// Just use the last message from the AI.
	// we don't want to re-send the entire conversation as that'll increase the chances of hiting the rate limit.
	// lastConverstion := conversation[len(conversation)-1:]

	for _, format := range lo.Uniq(spec.Formats) {
		switch format {
		case v1.AIActionFormatSlack:
			response, _, err := llm.PromptWithHistory(ctx, llmConf, conversation, slackPrompt)
			if err != nil {
				return nil, fmt.Errorf("failed to generate slack message: %w", err)
			}
			result.Slack = response

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
			if err := recommendPlaybookPrompt.Execute(&prompt, map[string]any{
				"base_url":  api.FrontendURL,
				"playbooks": string(playbooksJSON),
			}); err != nil {
				return nil, err
			}

			response, _, err := llm.PromptWithHistory(ctx, llmConf, conversation, prompt.String())
			if err != nil {
				return nil, fmt.Errorf("failed to generate playbook recommendation: %w", err)
			}
			result.RecommendedPlaybooks = response
		}
	}

	return &result, nil
}

func buildPrompt(ctx context.Context, prompt string, spec v1.AIActionContext) ([]string, error) {
	knowledge, err := getKnowledgeGraph(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt context: %w", err)
	}

	knowledgeJSON, err := json.MarshalIndent(knowledge, "", "\t")
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt context: %w", err)
	}

	output := []string{prompt, jsonBlock(string(knowledgeJSON))}
	if ctx.Properties().On(false, "playbook.action.ai.log-prompt") {
		fmt.Println(output)
	}

	return output, nil
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
