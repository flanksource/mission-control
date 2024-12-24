package actions

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/llm"
	"github.com/samber/lo"
)

type AIAction struct{}

type AIActionResult struct {
	Stdout string `json:"stdout"` // TODO: only naming this stdout because the frontend has proper formatted display for this field
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
	response, err := llm.Prompt(ctx, llmConf, prompt)
	if err != nil {
		return nil, err
	}

	return &AIActionResult{Stdout: response}, nil
}

func buildPrompt(ctx context.Context, prompt string, spec v1.AIActionContext) (string, error) {
	config, err := query.GetCachedConfig(ctx, spec.Config)
	if err != nil {
		return "", fmt.Errorf("failed to get config (%s): %w", spec.Config, err)
	} else if config == nil {
		return "", fmt.Errorf("config doesn't exist  (%s): %w", spec.Config, err)
	}

	output := fmt.Sprintf("Config: %s\n", codeblock(string(lo.FromPtr(config.Config))))

	for _, relationship := range spec.Relationships {
		relatedConfigs, err := query.GetRelatedConfigs(ctx, relationship.ToRelationshipQuery(config.ID))
		if err != nil {
			return "", fmt.Errorf("failed to get related config (%s): %w", config.ID, err)
		}

		if len(relatedConfigs) == 0 {
			continue
		}

		relatedConfigsJSON, err := json.Marshal(relatedConfigs)
		if err != nil {
			return "", err
		}

		output += fmt.Sprintf("\n\nHere are all the %s related configs down to depth=%d: %s",
			relationship.Direction,
			lo.FromPtr(relationship.Depth),
			codeblock(string(relatedConfigsJSON)),
		)
	}

	output += fmt.Sprintf("\n\n----\n\n%s", prompt)
	fmt.Println(output)
	return output, nil
}

func codeblock(code string) string {
	return fmt.Sprintf("\n```json\n%s\n```\n", code)
}
