package actions

import (
	"fmt"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/llm"
	"github.com/samber/lo"
)

type promptContext struct {
	config          *models.ConfigItem
	relatedConfigs  []query.RelatedConfig
	relatedChanges  []query.ConfigChangeRow
	analysisResults []models.ConfigAnalysis
}

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
	response, err := llm.Prompt(ctx, llmConf, spec.SystemPrompt, prompt)
	if err != nil {
		return nil, err
	}

	return &AIActionResult{Logs: response}, nil
}

func buildPrompt(ctx context.Context, prompt string, spec v1.AIActionContext) (string, error) {
	pctx, err := getPromptContext(ctx, spec)
	if err != nil {
		return "", fmt.Errorf("failed to get prompt context: %w", err)
	}

	paragraphs := []string{
		prompt,
		fmt.Sprintf("here's the config:\n%s", jsonBlock(string(lo.FromPtr(pctx.config.Config)))),
		formatRelatedConfigs(pctx.relatedConfigs),
		formatRelatedConfigsGraph(pctx.relatedConfigs),
		formatAnalyses(pctx.analysisResults),
		formatChanges(pctx.config.ID.String(), pctx.relatedChanges),
	}

	paragraphs = lo.Filter(paragraphs, func(s string, _ int) bool { return s != "" })
	output := strings.Join(paragraphs, "\n\n")
	return output, nil
}

func getPromptContext(ctx context.Context, spec v1.AIActionContext) (*promptContext, error) {
	config, err := query.GetCachedConfig(ctx, spec.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to get config (%s): %w", spec.Config, err)
	} else if config == nil {
		return nil, fmt.Errorf("config doesn't exist (%s)", spec.Config)
	}

	pctx := &promptContext{
		config: config,
	}

	if spec.ShouldFetchConfigChanges() {
		response, err := query.FindCatalogChanges(ctx, query.CatalogChangesSearchRequest{
			CatalogID: config.ID.String(),
			Recursive: query.CatalogChangeRecursiveNone,
			From:      fmt.Sprintf("now-%s", spec.Changes.Since),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get config changes (%s): %w", config.ID, err)
		}
		pctx.relatedChanges = append(pctx.relatedChanges, response.Changes...)
	}

	if spec.Analysis.Since != "" {
		var analyses []models.ConfigAnalysis
		if err := ctx.DB().Where("config_id = ?", config.ID.String()).Find(&analyses).Error; err != nil {
			return nil, fmt.Errorf("failed to get config analysis: %w", err)
		}
		pctx.analysisResults = append(pctx.analysisResults, analyses...)
	}

	for _, relationship := range spec.Relationships {
		relatedConfigs, err := query.GetRelatedConfigs(ctx, relationship.ToRelationshipQuery(config.ID))
		if err != nil {
			return nil, fmt.Errorf("failed to get related config (%s): %w", config.ID, err)
		}
		pctx.relatedConfigs = append(pctx.relatedConfigs, relatedConfigs...)

		if relationship.Changes.Since != "" {
			response, err := query.FindCatalogChanges(ctx, query.CatalogChangesSearchRequest{
				CatalogID: config.ID.String(),
				Depth:     lo.FromPtr(relationship.Depth),
				Recursive: relationship.Direction.ToChangeDirection(),
				From:      fmt.Sprintf("now-%s", relationship.Changes.Since),
			})
			if err != nil {
				return nil, fmt.Errorf("failed to get config changes (%s): %w", config.ID, err)
			}
			pctx.relatedChanges = append(pctx.relatedChanges, response.Changes...)
		}

		if len(relatedConfigs) > 0 && relationship.Analysis.Since != "" {
			relatedConfigIDs := lo.Map(relatedConfigs, func(c query.RelatedConfig, _ int) string {
				return c.ID.String()
			})

			var analyses []models.ConfigAnalysis
			if err := ctx.DB().Where("config_id IN ?", relatedConfigIDs).Find(&analyses).Error; err != nil {
				return nil, fmt.Errorf("failed to get config analysis: %w", err)
			}
			pctx.analysisResults = append(pctx.analysisResults, analyses...)
		}
	}

	return pctx, nil
}

func formatChanges(configID string, changes []query.ConfigChangeRow) string {
	if len(changes) == 0 {
		return ""
	}

	var outputs []string
	var configChanges []string
	var relatedConfigChanges []string

	seen := make(map[string]struct{})
	for _, c := range changes {
		if _, ok := seen[c.ID]; ok {
			continue
		}

		seen[c.ID] = struct{}{}
		if c.ConfigID == configID {
			configChanges = append(configChanges, fmt.Sprintf("%s | %s | %s | %s", c.ChangeType, c.Summary, c.Source, c.CreatedAt))
		} else {
			relatedConfigChanges = append(relatedConfigChanges, fmt.Sprintf("%s | %s | %s | %s | %s", c.ConfigID, c.ChangeType, c.Summary, c.Source, c.CreatedAt))
		}
	}

	if len(configChanges) > 0 {
		lines := append([]string{
			"here are the last few changes for the config:\n",
			"change type | summary | source | created at",
			"---         | ---     | ---    | ---",
		}, configChanges...)
		outputs = append(outputs, strings.Join(lines, "\n"))
	}

	if len(relatedConfigChanges) > 0 {
		lines := append([]string{
			"here are the last few changes for the related configs:\n",
			"resource | change type | summary | source | created at",
			"---      | ---         | ---     | ---    | ---",
		}, relatedConfigChanges...)
		outputs = append(outputs, strings.Join(lines, "\n"))
	}

	return strings.Join(outputs, "\n\n")
}

func formatRelatedConfigs(configs []query.RelatedConfig) string {
	if len(configs) == 0 {
		return ""
	}

	seen := make(map[string]struct{})
	lines := []string{
		"here are the related configs:\n",
		" id | name | type | created at | updated at | changes | status | health",
		"--- | ---  | ---  | ---        | ---        | ---     | ---    | ---",
	}
	for _, c := range configs {
		if _, ok := seen[c.ID.String()]; ok {
			continue
		}

		lines = append(lines, fmt.Sprintf("%s | %s | %s | %s | %s | %d | %s | %s",
			c.ID.String(), c.Name, c.Type, c.CreatedAt, c.UpdatedAt, c.Changes, lo.FromPtr(c.Status), lo.FromPtr(c.Health),
		))
		seen[c.ID.String()] = struct{}{}
	}

	return strings.Join(lines, "\n")
}

func formatRelatedConfigsGraph(configs []query.RelatedConfig) string {
	if len(configs) == 0 {
		return ""
	}

	nodes := make(map[string]struct{})
	for _, c := range configs {
		nodes[c.ID.String()] = struct{}{}
	}

	lines := []string{
		"here's how these configs are related:\n",
		"parent | child",
		"--- | ---",
	}
	for _, c := range configs {
		for _, relatedID := range c.RelatedIDs {
			if _, ok := nodes[relatedID]; !ok {
				continue
			}

			lines = append(lines, fmt.Sprintf("%s | %s", c.ID.String(), relatedID))
		}
	}

	return strings.Join(lines, "\n")
}

func formatAnalyses(analyses []models.ConfigAnalysis) string {
	if len(analyses) == 0 {
		return ""
	}

	seen := make(map[string]struct{})
	lines := []string{
		"here are the analysis for the configs:\n",
		"config_id | source | analyzer | analysis | severity | summary | message | first observed",
		"      --- | ---    | ---      | ---      | ---      | ---     |     --- | ---",
	}
	for _, a := range analyses {
		if _, ok := seen[a.ID.String()]; ok {
			continue
		}
		seen[a.ID.String()] = struct{}{}

		lines = append(lines, fmt.Sprintf("%s | %s | %s | %s | %s | %s | %s | %s",
			a.ConfigID, a.Source, a.Analyzer, a.Analysis, a.Severity, a.Summary, a.Message, a.FirstObserved))
	}

	return strings.Join(lines, "\n")
}

func jsonBlock(code string) string {
	const format = "```json\n%s\n```"
	return fmt.Sprintf(format, code)
}
