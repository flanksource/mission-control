package actions

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/llm"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

type AIAction struct{}

type AIActionResult struct {
	Stdout string `json:"stdout"` // TODO: only naming this stdout because the frontend has proper formatted display for this field
}

func (t *AIAction) Run(ctx context.Context, spec v1.AIAction) (*AIActionResult, error) {
	if spec.Backend == "" {
		spec.Backend = api.LLMBackendAnthropic
	}

	apiKey, err := ctx.GetEnvValueFromCache(spec.APIKey, ctx.GetNamespace())
	if err != nil {
		return nil, err
	}

	prompt, err := buildPrompt(ctx, spec.Prompt, spec.AIActionContext)
	if err != nil {
		return nil, fmt.Errorf("failed to form prompt: %w", err)
	}

	fmt.Println(prompt)

	llmConf := llm.Config{APIKey: apiKey, Backend: spec.Backend, Model: spec.Model}
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

	if spec.Relationships != nil {
		relatedConfigs, err := query.GetRelatedConfigs(ctx, query.RelationQuery{MaxDepth: &spec.Relationships.Depth, ID: config.ID})
		if err != nil {
			return "", fmt.Errorf("failed to get related config (%s): %w", config.ID, err)
		}

		if len(relatedConfigs) > 0 {
			relatedConfigsJSON, err := json.Marshal(relatedConfigs)
			if err != nil {
				return "", err
			}

			output += fmt.Sprintf("\n\nHere are the related configs: %s", codeblock(string(relatedConfigsJSON)))

			relatedConfigIDs := lo.Map(relatedConfigs, func(c query.RelatedConfig, _ int) uuid.UUID {
				return c.ID
			})

			relatedConfigDetails, err := query.GetConfigsByIDs(ctx, relatedConfigIDs)
			if err != nil {
				return "", err
			}

			// TODO: just one input for the related configs
			relatedConfigDetailsJSON, err := json.Marshal(relatedConfigDetails)
			if err != nil {
				return "", err
			}
			output += fmt.Sprintf("\n\nHere are the related config details: %s", codeblock(string(relatedConfigDetailsJSON)))
		}
	}

	output += fmt.Sprintf("\n\n----\n\n%s", prompt)
	return output, nil
}

func codeblock(code string) string {
	return fmt.Sprintf("\n```json\n%s\n```\n", code)
}
