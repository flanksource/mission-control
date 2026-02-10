package actions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/template"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/shutdown"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"
	"github.com/google/uuid"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	pkgArtifacts "github.com/flanksource/incident-commander/artifacts"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/llm"
	llmContext "github.com/flanksource/incident-commander/llm/context"
	"github.com/flanksource/incident-commander/utils"
)

// We don't want to include large files in the LLM context.
const maxArtifactSize = 5 * 1024 * 1024 // 5MB

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

// aiAction represents an action that uses AI to analyze configurations and recommend playbooks.
type aiAction struct {
	PlaybookID  uuid.UUID // ID of the playbook that is executing this action
	RunID       uuid.UUID // ID of the run that is executing this action
	ActionID    uuid.UUID // ID of the action that is executing this action
	TemplateEnv TemplateEnv
}

func NewAIAction(playbookID, runID, actionID uuid.UUID, templateEnv TemplateEnv) *aiAction {
	return &aiAction{
		PlaybookID:  playbookID,
		RunID:       runID,
		ActionID:    actionID,
		TemplateEnv: templateEnv,
	}
}

// AIActionResult contains the results of an AI action execution.
type AIActionResult struct {
	JSON                 string `json:"json,omitempty"`                 // JSON formatted diagnosis report
	Markdown             string `json:"markdown,omitempty"`             // Markdown formatted diagnosis report
	Slack                string `json:"slack,omitempty"`                // Slack blocks formatted diagnosis report
	RecommendedPlaybooks string `json:"recommendedPlaybooks,omitempty"` // Recommended playbooks in Slack blocks format

	// GenerationInfo about the all the LLM calls
	GenerationInfo []llm.GenerationInfo `json:"generationInfo,omitempty"`

	// Prompt can get very large so we don't want to store it in the database.
	// It's stored as an artifact instead.
	Prompt strings.Builder `json:"-"`

	ChildRunsTriggered int `json:"-"`
}

func (t *AIActionResult) GetArtifacts() []artifacts.Artifact {
	if t.Prompt.Len() == 0 {
		// Prompt can be empty when executing child runs.
		return nil
	}

	return []artifacts.Artifact{
		{
			ContentType: "text/markdown",
			Content:     io.NopCloser(strings.NewReader(t.Prompt.String())),
			Path:        "prompt.md",
		},
	}
}

func (e *AIActionResult) GetStatus() models.PlaybookActionStatus {
	if e.ChildRunsTriggered > 0 {
		return models.PlaybookActionStatusWaitingChildren
	}

	return models.PlaybookActionStatusCompleted
}

type childRunResultContext struct {
	Playbook string          `json:"playbook"`
	Results  []types.JSONMap `json:"results"`
}

// Run executes the AI action with the given specification.
// It builds a prompt using the knowledge graph, sends it to the LLM,
// and processes the response into the requested formats.
func (t *aiAction) Run(ctx context.Context, spec v1.AIAction) (*AIActionResult, error) {
	var result AIActionResult
	knowledgebase, prompt, err := buildPrompt(ctx, spec.Prompt, spec.LLMContextRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to form prompt: %w", err)
	}

	if err := spec.AIActionClient.Populate(ctx); err != nil {
		return nil, fmt.Errorf("failed to populate llm client connection: %w", err)
	}

	if spec.DryRun {
		return &AIActionResult{Markdown: strings.Join(prompt, "\n")}, nil
	}

	if len(spec.LLMContextRequest.Playbooks) > 0 {
		var childRuns []models.PlaybookRun
		if err := ctx.DB().Where("parent_id = ?", t.RunID).Find(&childRuns).Error; err != nil {
			return nil, fmt.Errorf("failed to get child playbook runs: %w", err)
		}

		// If child runs were already triggered, read the results from all of these child runs & add to the LLM's context
		// else trigger them now.
		if len(childRuns) > 0 {
			// Read the results from all of these child runs & add to the LLM's context
			childRunResults, err := getChildRunsResults(ctx, childRuns)
			if err != nil {
				return nil, fmt.Errorf("failed to get child run results: %w", err)
			}

			if len(childRunResults) > 0 {
				childRunResultsJSON, err := json.Marshal(childRunResults)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal child run results: %w", err)
				}

				prompt = append(prompt, fmt.Sprintf("Here are the results from the child playbook runs that may be relevant to the issue: %s", string(childRunResultsJSON)))
			}
		} else {
			for _, contextProvider := range spec.LLMContextRequest.Playbooks {
				if contextProvider.If != "" {
					if pass, err := gomplate.RunTemplateBool(t.TemplateEnv.AsMap(ctx), gomplate.Template{Expression: contextProvider.If}); err != nil {
						return nil, fmt.Errorf("failed to evaluate if condition for context provider playbook %s: %w", contextProvider.Name, err)
					} else if !pass {
						continue
					}
				}

				if err := t.triggerPlaybookRun(ctx, contextProvider); err != nil {
					return nil, fmt.Errorf("failed to trigger context provider playbook %s: %w", contextProvider.Name, err)
				}

				result.ChildRunsTriggered++
			}

			if result.ChildRunsTriggered > 0 {
				// If child runs were triggered, then this (parent) run goes into a "awaiting children" state.
				// A job monitors if all the child playbook runs have completed & resumes this run.
				return &result, nil
			}
		}
	}

	llmConf := llm.Config{AIActionClient: spec.AIActionClient, ResponseFormat: llm.ResponseFormatDiagnosis}
	response, conversation, genInfo, err := llm.Prompt(ctx, llmConf, spec.SystemPrompt, prompt...)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to generate response")
	}
	result.Prompt.WriteString(strings.Join(prompt, "\n"))
	result.JSON = response
	result.GenerationInfo = append(result.GenerationInfo, genInfo...)

	var diagnosisReport llm.DiagnosisReport
	if err := json.Unmarshal([]byte(response), &diagnosisReport); err != nil {
		return nil, ctx.Oops().With("response", response).Wrapf(err, "failed to unmarshal diagnosis report")
	}

	for _, format := range lo.Uniq(spec.Formats) {
		switch format {
		case v1.AIActionFormatSlack:
			groupedResources, err := getGroupedResources(ctx, t.RunID)
			if err != nil {
				return nil, fmt.Errorf("failed to get grouped resources: %w", err)
			}

			result.Slack, err = formatDiagnosisReportAsSlackBlocks(ctx, knowledgebase, diagnosisReport, llm.PlaybookRecommendations{}, groupedResources)
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

			playbooksJSON, err := json.Marshal(supportedPlaybooks)
			if err != nil {
				return nil, err
			}

			var prompt bytes.Buffer
			if err := recommendPlaybookPrompt.Execute(&prompt, map[string]any{"playbooks": string(playbooksJSON)}); err != nil {
				return nil, err
			}

			llmConf.ResponseFormat = llm.ResponseFormatPlaybookRecommendations
			response, _, genInfo, err := llm.PromptWithHistory(ctx, llmConf, conversation, prompt.String())
			if err != nil {
				return nil, fmt.Errorf("failed to generate playbook recommendation: %w", err)
			}
			result.Prompt.WriteString(prompt.String())
			result.GenerationInfo = append(result.GenerationInfo, genInfo...)

			var recommendations llm.PlaybookRecommendations
			if err := json.Unmarshal([]byte(response), &recommendations); err != nil {
				return nil, ctx.Oops().With("response", response).Wrapf(err, "failed to unmarshal playbook recommendations")
			}

			groupedResources, err := getGroupedResources(ctx, t.RunID)
			if err != nil {
				return nil, fmt.Errorf("failed to get grouped resources: %w", err)
			}

			blocks, err := formatDiagnosisReportAsSlackBlocks(ctx, knowledgebase, diagnosisReport, recommendations, groupedResources)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal blocks: %w", err)
			}
			result.RecommendedPlaybooks = string(blocks)
		}
	}

	return &result, nil
}

// triggerPlaybookRun creates an event to trigger a playbook run.
// The status of the playbook run is then handled entirely by playbook.
func (t *aiAction) triggerPlaybookRun(ctx context.Context, contextProvider api.LLMContextRequestPlaybook) error {
	parametersJSON, err := json.Marshal(contextProvider.Params)
	if err != nil {
		return fmt.Errorf("failed to marshal parameters: %w", err)
	}

	var playbook models.Playbook
	if err := ctx.DB().Where("name = ?", contextProvider.Name).
		Where("namespace = ?", contextProvider.Namespace).
		Where("deleted_at IS NULL").
		Find(&playbook).Error; err != nil {
		return fmt.Errorf("failed to find playbook %s/%s: %w", contextProvider.Namespace, contextProvider.Name, err)
	} else if playbook.ID == uuid.Nil {
		return fmt.Errorf("playbook %s/%s not found", contextProvider.Namespace, contextProvider.Name)
	}

	eventProp := types.JSONStringMap{
		"id":            playbook.ID.String(),
		"playbook_id":   playbook.ID.String(),
		"parent_run_id": t.RunID.String(),
		"parameters":    string(parametersJSON),
	}

	if t.TemplateEnv.Config != nil && t.TemplateEnv.Config.ID != uuid.Nil {
		eventProp["config_id"] = t.TemplateEnv.Config.ID.String()
	} else if t.TemplateEnv.Component != nil && t.TemplateEnv.Component.ID != uuid.Nil {
		eventProp["component_id"] = t.TemplateEnv.Component.ID.String()
	} else if t.TemplateEnv.Check != nil && t.TemplateEnv.Check.ID != uuid.Nil {
		eventProp["check_id"] = t.TemplateEnv.Check.ID.String()
	}

	event := models.Event{
		Name:       api.EventPlaybookRun,
		EventID:    uuid.NewSHA1(t.ActionID, []byte(playbook.ID.String())),
		Properties: eventProp,
	}
	if err := ctx.DB().Create(&event).Error; err != nil {
		return fmt.Errorf("failed to create run: %w", err)
	}

	return nil
}

// buildPrompt constructs a prompt for the LLM by combining the user prompt with knowledge graph data.
// It returns the knowledge graph, the complete prompt array, and any error encountered.
func buildPrompt(ctx context.Context, prompt string, spec api.LLMContextRequest) (*llmContext.Context, []string, error) {
	knowledge, err := llmContext.Create(ctx, spec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get prompt context: %w", err)
	}

	knowledgeJSON, err := json.Marshal(knowledge)
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

// getGroupedResources returns the list of grouped resources for the given run ID.
// If this run was triggered by a notification.
func getGroupedResources(ctx context.Context, runID uuid.UUID) ([]string, error) {
	// Note: when running on agents, the run will not be in the database.
	// i.e. if this ai action is running on an agent, it'll not have access to the list of grouped resources.
	var run models.PlaybookRun
	if err := ctx.DB().Select("notification_send_id").Where("id = ?", runID).Limit(1).Find(&run).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to get playbook run")
	}

	if run.NotificationSendID == nil {
		return nil, nil
	}

	var notificationSend models.NotificationSendHistory
	if err := ctx.DB().Where("id = ?", *run.NotificationSendID).Limit(1).Find(&notificationSend).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to get notification send history")
	}

	if notificationSend.GroupID == nil {
		return nil, nil
	}

	var selfID []string
	if run.ConfigID != nil {
		selfID = append(selfID, lo.FromPtr(run.ConfigID).String())
	} else if run.CheckID != nil {
		selfID = append(selfID, lo.FromPtr(run.CheckID).String())
	} else if run.ComponentID != nil {
		selfID = append(selfID, lo.FromPtr(run.ComponentID).String())
	}

	groupedResources, err := db.GetGroupedResources(ctx, *notificationSend.GroupID, selfID...)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to get grouped resources")
	}

	return groupedResources, nil
}

func getChildRunsResults(ctx context.Context, childRuns []models.PlaybookRun) ([]childRunResultContext, error) {
	var childRunResults []childRunResultContext

	for _, childRun := range childRuns {
		actions, err := childRun.GetActions(ctx.DB())
		if err != nil {
			return nil, fmt.Errorf("failed to get child playbook run actions: %w", err)
		}

		playbook, err := childRun.GetPlaybook(ctx.DB())
		if err != nil {
			return nil, fmt.Errorf("failed to get child playbook run playbook: %w", err)
		}

		actionIDs := lo.Map(actions, func(action models.PlaybookRunAction, _ int) string {
			return action.ID.String()
		})

		artifacts, err := pkgArtifacts.GetArtifactContents(ctx, actionIDs...)
		if err != nil {
			return nil, fmt.Errorf("failed to get artifact contents: %w", err)
		}

		var actionResult []types.JSONMap
		for _, action := range actions {
			actionResult = append(actionResult, action.Result)

			artifact, ok := lo.Find(artifacts, func(a pkgArtifacts.ArtifactContent) bool {
				return a.ActionID == action.ID.String()
			})
			if ok {
				content := utils.Tail(artifact.Content, maxArtifactSize)
				actionResult = append(actionResult, types.JSONMap{"artifact": content})
			}
		}

		childRunResults = append(childRunResults, childRunResultContext{
			Playbook: playbook.Name,
			Results:  actionResult,
		})
	}

	return childRunResults, nil
}
