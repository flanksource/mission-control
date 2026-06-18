package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/samber/lo"
	"github.com/tmc/langchaingo/llms"
)

// _converseModel implements llms.Model using the Bedrock Converse API,
// which is provider-agnostic and works with any model ID on Bedrock.
type _converseModel struct {
	client  *bedrockruntime.Client
	modelID string
}

var _ llms.Model = (*_converseModel)(nil)

func newConverseModel(client *bedrockruntime.Client, modelID string) *_converseModel {
	return &_converseModel{client: client, modelID: modelID}
}

func (m *_converseModel) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, m, prompt, options...)
}

func (m *_converseModel) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	opts := &llms.CallOptions{}
	for _, opt := range options {
		opt(opts)
	}

	var systemPrompts []types.SystemContentBlock
	var bedrockMessages []types.Message
	var lastRole types.ConversationRole

	for _, msg := range messages {
		if msg.Role == llms.ChatMessageTypeSystem {
			for _, part := range msg.Parts {
				if text, ok := part.(llms.TextContent); ok {
					systemPrompts = append(systemPrompts, &types.SystemContentBlockMemberText{Value: text.Text})
				}
			}
			continue
		}

		role := langchainRoleToConverse(msg.Role)
		contentBlocks := langchainPartsToContentBlocks(msg.Parts)

		// Merge consecutive messages with the same role (Converse requires alternating roles)
		if len(bedrockMessages) > 0 && bedrockMessages[len(bedrockMessages)-1].Role == role {
			bedrockMessages[len(bedrockMessages)-1].Content = append(
				bedrockMessages[len(bedrockMessages)-1].Content, contentBlocks...,
			)
		} else {
			bedrockMessages = append(bedrockMessages, types.Message{
				Role:    role,
				Content: contentBlocks,
			})
		}
		lastRole = role
	}

	// Converse requires first message to be user
	if len(bedrockMessages) > 0 && bedrockMessages[0].Role != types.ConversationRoleUser {
		bedrockMessages[0].Role = types.ConversationRoleUser
	}

	// If last message is assistant (e.g. from conversation history), add an empty user turn
	if lastRole == types.ConversationRoleAssistant {
		bedrockMessages = append(bedrockMessages, types.Message{
			Role:    types.ConversationRoleUser,
			Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: ""}},
		})
	}

	input := &bedrockruntime.ConverseInput{
		ModelId:  aws.String(m.modelID),
		Messages: bedrockMessages,
		System:   systemPrompts,
	}

	if opts.MaxTokens > 0 {
		m := int32(opts.MaxTokens)
		input.InferenceConfig = &types.InferenceConfiguration{MaxTokens: &m}
	}
	if opts.Temperature > 0 {
		t := float32(opts.Temperature)
		if input.InferenceConfig == nil {
			input.InferenceConfig = &types.InferenceConfiguration{}
		}
		input.InferenceConfig.Temperature = &t
	}
	if len(opts.Tools) > 0 {
		input.ToolConfig = convertToolsToConverse(opts.Tools)
	}

	output, err := m.client.Converse(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("bedrock Converse: %w", err)
	}

	return convertConverseOutput(output)
}

func langchainRoleToConverse(role llms.ChatMessageType) types.ConversationRole {
	switch role {
	case llms.ChatMessageTypeAI:
		return types.ConversationRoleAssistant
	default:
		return types.ConversationRoleUser
	}
}

func langchainPartsToContentBlocks(parts []llms.ContentPart) []types.ContentBlock {
	var blocks []types.ContentBlock
	for _, part := range parts {
		switch p := part.(type) {
		case llms.TextContent:
			blocks = append(blocks, &types.ContentBlockMemberText{Value: p.Text})
		case llms.ToolCall:
			inputDoc, _ := marshalToDocument(p.FunctionCall.Arguments)
			blocks = append(blocks, &types.ContentBlockMemberToolUse{
				Value: types.ToolUseBlock{
					ToolUseId: aws.String(p.ID),
					Name:      aws.String(p.FunctionCall.Name),
					Input:     inputDoc,
				},
			})
		case llms.ToolCallResponse:
			blocks = append(blocks, &types.ContentBlockMemberToolResult{
				Value: types.ToolResultBlock{
					ToolUseId: aws.String(p.ToolCallID),
					Content:   []types.ToolResultContentBlock{&types.ToolResultContentBlockMemberText{Value: p.Content}},
				},
			})
		}
	}
	return blocks
}

func convertToolsToConverse(tools []llms.Tool) *types.ToolConfiguration {
	bedrockTools := make([]types.Tool, len(tools))
	for i, t := range tools {
		schema, _ := marshalToDocument(t.Function.Parameters)
		bedrockTools[i] = &types.ToolMemberToolSpec{
			Value: types.ToolSpecification{
				Name:        aws.String(t.Function.Name),
				Description: aws.String(t.Function.Description),
				InputSchema: &types.ToolInputSchemaMemberJson{Value: schema},
			},
		}
	}
	return &types.ToolConfiguration{Tools: bedrockTools}
}

func convertConverseOutput(output *bedrockruntime.ConverseOutput) (*llms.ContentResponse, error) {
	msg, ok := output.Output.(*types.ConverseOutputMemberMessage)
	if !ok {
		return nil, errors.New("bedrock Converse: unexpected output type")
	}

	choice := &llms.ContentChoice{
		StopReason: string(output.StopReason),
		GenerationInfo: map[string]any{
			"InputTokens":  int(lo.FromPtr(output.Usage.InputTokens)),
			"OutputTokens": int(lo.FromPtr(output.Usage.OutputTokens)),
		},
	}

	for _, block := range msg.Value.Content {
		switch b := block.(type) {
		case *types.ContentBlockMemberText:
			choice.Content += b.Value
		case *types.ContentBlockMemberToolUse:
			choice.ToolCalls = append(choice.ToolCalls, llms.ToolCall{
				ID:   lo.FromPtr(b.Value.ToolUseId),
				Type: "function",
				FunctionCall: &llms.FunctionCall{
					Name:      lo.FromPtr(b.Value.Name),
					Arguments: unmarshalDocument(b.Value.Input),
				},
			})
		}
	}

	if len(choice.ToolCalls) > 0 {
		choice.FuncCall = choice.ToolCalls[0].FunctionCall
	}

	return &llms.ContentResponse{Choices: []*llms.ContentChoice{choice}}, nil
}

type _smithyMarshaler interface {
	MarshalSmithyDocument() ([]byte, error)
}

func unmarshalDocument(doc document.Interface) string {
	if doc == nil {
		return ""
	}
	if m, ok := doc.(_smithyMarshaler); ok {
		b, err := m.MarshalSmithyDocument()
		if err == nil {
			return string(b)
		}
	}
	return ""
}

func marshalToDocument(v any) (document.Interface, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return document.NewLazyDocument(doc), nil
}
