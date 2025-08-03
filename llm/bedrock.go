package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/samber/lo"
	"github.com/tmc/langchaingo/llms"
)

type BedrockModelWrapper struct {
	client         *bedrockruntime.Client
	modelID        string
	region         string
	ResponseFormat ResponseFormat
}

func NewBedrockModelWrapper(ctx context.Context, modelID, region string, respFmt ResponseFormat) (llms.Model, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	if region != "" {
		awsCfg.Region = region
	}
	client := bedrockruntime.NewFromConfig(awsCfg)
	return &BedrockModelWrapper{
		client:         client,
		modelID:        modelID,
		region:         awsCfg.Region,
		ResponseFormat: respFmt,
	}, nil
}

func (b *BedrockModelWrapper) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	resp, _, err := b.GenerateContent(ctx, []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, prompt),
	}, options...)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from Bedrock")
	}
	return resp.Choices[0].Content, nil
}

func (b *BedrockModelWrapper) GenerateContent(
	ctx context.Context,
	messages []llms.MessageContent,
	options ...llms.CallOption,
) (*llms.ContentResponse, error) {
	optsParsed := &llms.CallOptions{}
	for _, o := range options {
		o(optsParsed)
	}
	var promptMsgs []map[string]any
	for _, m := range messages {
		role := "user"
		if m.Role == llms.ChatMessageTypeAI {
			role = "assistant"
		}
		var contentBuilder strings.Builder
		for _, part := range m.Parts {
			if s, ok := part.(string); ok {
				contentBuilder.WriteString(s)
			}
		}
		content := contentBuilder.String()
		promptMsgs = append(promptMsgs, map[string]any{
			"role":    role,
			"content": content,
		})
	}
	payload := map[string]any{
		"anthropic_version": "bedrock-2023-05-31",
		"messages":          promptMsgs,
		"max_tokens":        1024,
	}
	if optsParsed.Temperature > 0 {
		payload["temperature"] = optsParsed.Temperature
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bedrock payload: %w", err)
	}
	in := &bedrockruntime.InvokeModelInput{
		ModelId:     lo.ToPtr(b.modelID),
		ContentType: lo.ToPtr("application/json"),
		Body:        body,
	}
	out, err := b.client.InvokeModel(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("bedrock InvokeModel failed: %w", err)
	}
	defer out.Body.Close()
	respBytes, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read bedrock response: %w", err)
	}
	var respData map[string]any
	if err := json.Unmarshal(respBytes, &respData); err != nil {
		return nil, fmt.Errorf("failed to decode bedrock response: %w", err)
	}
	var text string
	switch {
	case respData["completion"] != nil:
		text, _ = respData["completion"].(string)
	case respData["generations"] != nil:
		if gens, ok := respData["generations"].([]any); ok && len(gens) > 0 {
			if gen, ok := gens[0].(map[string]any); ok {
				text, _ = gen["text"].(string)
			}
		}
	}
	choice := llms.ContentChoice{
		Content:        text,
		GenerationInfo: map[string]any{},
	}
	if usage, ok := respData["usage"].(map[string]any); ok {
		if pt, ok := usage["prompt_tokens"]; ok {
			if v, ok := asInt(pt); ok {
				choice.GenerationInfo["InputTokens"] = v
			}
		}
		if ct, ok := usage["completion_tokens"]; ok {
			if v, ok := asInt(ct); ok {
				choice.GenerationInfo["OutputTokens"] = v
			}
		}
		if pt, ok := usage["input_tokens"]; ok {
			if v, ok := asInt(pt); ok {
				choice.GenerationInfo["InputTokens"] = v
			}
		}
		if ct, ok := usage["output_tokens"]; ok {
			if v, ok := asInt(ct); ok {
				choice.GenerationInfo["OutputTokens"] = v
			}
		}
	}
	return &llms.ContentResponse{
		Choices: []llms.ContentChoice{choice},
	}, nil
}

func asInt(val any) (int, bool) {
	switch t := val.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	case json.Number:
		i, err := t.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}