package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/sethvargo/go-retry"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/events"
)

const PlaybookToolName = "executePlaybook"

type PlaybookRunRequest struct {
	ID         string         `json:"id"`
	Parameters map[string]any `json:"parameters"`
}

type PlaybookRunResponse struct {
	Run     *models.PlaybookRun        `json:"run"`
	Actions []models.PlaybookRunAction `json:"actions"`
}

func NewPlaybookRunner(ctx dutyContext.Context, configID string, parentPlaybookID, playbookRunID uuid.UUID) *PlaybookTool {
	return &PlaybookTool{
		dutyCtx:          ctx,
		configID:         configID,
		parentPlaybookID: parentPlaybookID.String(),
		parentRunID:      playbookRunID.String(),
	}
}

type PlaybookTool struct {
	dutyCtx  dutyContext.Context
	configID string

	parentPlaybookID string
	parentRunID      string
}

func (t *PlaybookTool) Name() string {
	return PlaybookToolName
}

func (t *PlaybookTool) Description() string {
	return `
	Executes a playbook with specified parameters. 
	A playbook is a predefined sequence of actions that can be executed with customizable parameters.

	<input>
	A JSON payload containing:
	- id: UUID of the playbook to execute (required)
	- parameters: Key-value pairs of parameters needed by the playbook (optional)

	Example input:
	{
		"id": "123e4567-e89b-12d3-a456-426614174000",
		"parameters": {
			"replicas": 2,
			"namespace": "default",
			"timeout": "5m"
		}
	}
	</input>

	<output>
	Returns a JSON response containing the execution results:
	- run: the playbook run that was executed
	- actions: actions are each steps in the playbook run. An action contains an error or a result on success.

	Example success output:
	{
		"run": {
			"status": "completed"
		},
		"actions": [
			{
				"name": "scaling deployment",
				"result": "successfully scaled",
				"status": "completed"
			}
		]
	}

	Example error output:
	{
		"run": {
			"status": "error"
		},
		"actions": [
			{
				"name": "scaling deployment",
				"result": "could not find deployment",
				"status": "error"
			}
		]
	}
	</output>
`
}

// TODO: handle error. agent will keep calling this tool. we need to indicate the agent to stop.
func (t *PlaybookTool) Call(ctx context.Context, input string) (string, error) {
	var req PlaybookRunRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", err
	}

	playbook, err := query.FindPlaybook(t.dutyCtx, req.ID, query.GetterOptionNoCache)
	if err != nil {
		return "", err
	} else if playbook == nil {
		return "", fmt.Errorf("playbook (%s) not found", req.ID)
	}

	if err := createPlaybookRunEvent(req, t.configID, t.parentPlaybookID, t.parentRunID); err != nil {
		return "", fmt.Errorf("failed to trigger playbook (%s): %w", req.ID, err)
	}

	timeout := time.Minute * 5
	childRun, err := waitPlaybookCompletion(t.dutyCtx, t.parentRunID, timeout)
	if err != nil {
		return "", fmt.Errorf("error waiting for child playbook completion: %w", err)
	}

	var actions []models.PlaybookRunAction
	if err := t.dutyCtx.DB().Select("name", "status", "result", "error").Where("playbook_run_id = ?", childRun.ID).
		Find(&actions).Error; err != nil {
		return "", fmt.Errorf("failed to get actions of the child run: %w", err)
	}

	output := PlaybookRunResponse{
		Run:     childRun,
		Actions: actions,
	}
	outputJSON, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("failed to marshal output for tool: Playbook: %w", err)
	}

	return string(outputJSON), nil
}

func waitPlaybookCompletion(ctx dutyContext.Context, parentRunID string, timeout time.Duration) (*models.PlaybookRun, error) {
	var run models.PlaybookRun
	backoff := retry.WithMaxDuration(timeout, retry.NewConstant(time.Second*3))
	err := retry.Do(ctx, backoff, func(_ctx context.Context) error {
		if err := ctx.DB().Where("parent_id = ?", parentRunID).Find(&run).Error; err != nil {
			return err
		}

		if run.ID == uuid.Nil {
			return retry.RetryableError(errors.New("run not found"))
		}

		if !lo.Contains(models.PlaybookRunStatusFinalStates, run.Status) {
			return retry.RetryableError(errors.New("playbook is still running. waiting for completion"))
		}

		return nil
	})

	return &run, err
}

func createPlaybookRunEvent(req PlaybookRunRequest, configID, parentPlaybookID, runID string) error {
	eventProp := types.JSONStringMap{
		"id":                 req.ID,
		"parent_playbook_id": parentPlaybookID,
		"parent_run_id":      runID,
		"config_id":          configID,
	}

	if len(req.Parameters) != 0 {
		jsonParams, err := json.Marshal(req.Parameters)
		if err != nil {
			return err
		}
		eventProp["params"] = string(jsonParams)
	}

	event := models.Event{
		Name:       api.EventPlaybookRun,
		Properties: eventProp,
	}

	// NOTE: We cannot create a `playbook.run` event in here to trigger a child playbook run
	// because this entire action is run in a transaction.
	// We'll keep waiting for the child playbook run to complete whereas the event never gets created
	// before we commit.
	// if err := ctx.DB().Create(&event).Error; err != nil {
	// 	return fmt.Errorf("failed to create run: %w", err)
	// }

	// Create the event by sending to this channel instead.
	// Not a fan of this. Need to think of something better.
	events.EventChan <- event

	return nil
}
