package events

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/gomplate/v3"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook"
)

func NewPlaybookApprovalSpecUpdatedConsumerSync() SyncEventConsumer {
	return SyncEventConsumer{
		watchEvents: []string{EventPlaybookSpecApprovalUpdated},
		consumers:   []SyncEventHandlerFunc{onApprovalUpdated},
	}
}

func NewPlaybookApprovalConsumerSync() SyncEventConsumer {
	return SyncEventConsumer{
		watchEvents: []string{EventPlaybookApprovalInserted},
		consumers:   []SyncEventHandlerFunc{onPlaybookRunNewApproval},
	}
}

type EventResource struct {
	Component    *models.Component    `json:"component,omitempty"`
	Check        *models.Check        `json:"check,omitempty"`
	CheckSummary *models.CheckSummary `json:"check_summary,omitempty"`
	Canary       *models.Canary       `json:"canary,omitempty"`
}

func (t *EventResource) AsMap() map[string]any {
	return map[string]any{
		"component":     t.Component,
		"check":         t.Check,
		"canary":        t.Canary,
		"check_summary": t.CheckSummary,
	}
}

func schedulePlaybookRun(ctx *api.Context, event api.Event) error {
	specEvent, ok := eventToSpecEvent[event.Name]
	if !ok {
		return nil
	}

	playbooks, err := playbook.FindPlaybooksForEvent(ctx, specEvent.Class, specEvent.Event)
	if err != nil {
		return fmt.Errorf("error fetching playbooks: %w", err)
	}

	if len(playbooks) == 0 {
		return nil
	}

	var eventResource EventResource
	switch event.Name {
	case EventCheckFailed, EventCheckPassed:
		checkID := event.Properties["id"]
		if err := ctx.DB().Where("id = ?", checkID).First(&eventResource.Check).Error; err != nil {
			return api.Errorf(api.ENOTFOUND, "check(id=%s) not found", checkID)
		}

		if summary, err := duty.CheckSummary(ctx, checkID); err != nil {
			return err
		} else if summary != nil {
			eventResource.CheckSummary = summary
		}

		if err := ctx.DB().Where("id = ?", eventResource.Check.CanaryID).First(&eventResource.Canary).Error; err != nil {
			return api.Errorf(api.ENOTFOUND, "canary(id=%s) not found", eventResource.Check.CanaryID)
		}

	case EventComponentStatusHealthy, EventComponentStatusUnhealthy, EventComponentStatusInfo, EventComponentStatusWarning, EventComponentStatusError:
		if err := ctx.DB().Model(&models.Component{}).Where("id = ?", event.Properties["id"]).First(&eventResource.Component).Error; err != nil {
			return api.Errorf(api.ENOTFOUND, "component(id=%s) not found", event.Properties["id"])
		}
	}

	for _, p := range playbooks {
		playbook, err := v1.PlaybookFromModel(p)
		if err != nil {
			logger.Errorf("error converting playbook model to spec: %s", err)
			logToJobHistory(ctx, p.ID.String(), err.Error())
			continue
		}

		run := models.PlaybookRun{
			PlaybookID: p.ID,
			Status:     models.PlaybookRunStatusPending,
		}

		if playbook.Spec.Approval == nil || playbook.Spec.Approval.Approvers.Empty() {
			run.Status = models.PlaybookRunStatusScheduled
		}

		switch specEvent.Class {
		case "canary":
			run.CheckID = &eventResource.Check.ID
			if ok, err := matchResource(eventResource.Check.Labels, eventResource.AsMap(), playbook.Spec.On.Canary); err != nil {
				logToJobHistory(ctx, p.ID.String(), err.Error())
				continue
			} else if ok {
				if err := ctx.DB().Create(&run).Error; err != nil {
					return err
				}
			}

		case "component":
			run.ComponentID = &eventResource.Component.ID
			if ok, err := matchResource(eventResource.Component.Labels, eventResource.AsMap(), playbook.Spec.On.Component); err != nil {
				logToJobHistory(ctx, p.ID.String(), err.Error())
				continue
			} else if ok {
				if err := ctx.DB().Create(&run).Error; err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// logToJobHistory logs any failures in saving a playbook run to the job history.
func logToJobHistory(ctx *api.Context, playbookID, err string) {
	jobHistory := models.NewJobHistory("SavePlaybookRun", "playbook", playbookID)
	jobHistory.Start()
	jobHistory.AddError(err)
	if err := db.PersistJobHistory(ctx, jobHistory.End()); err != nil {
		logger.Errorf("error persisting job history: %v", err)
	}
}

// matchResource returns true if any one of the matchFilter is true
// for the given labels and cel env.
func matchResource(labels map[string]string, celEnv map[string]any, matchFilters []v1.PlaybookEventDetail) (bool, error) {
outer:
	for _, mf := range matchFilters {
		if mf.Filter != "" {
			res, err := gomplate.RunTemplate(celEnv, gomplate.Template{Expression: mf.Filter})
			if err != nil {
				return false, err
			}

			if ok, err := strconv.ParseBool(res); err != nil {
				return false, api.Errorf(api.EINVALID, "expression (%s) didn't evaluate to a boolean value. got %s", mf.Filter, res)
			} else if !ok {
				continue outer
			}
		}

		for k, v := range mf.Labels {
			qVal, ok := labels[k]
			if !ok {
				continue outer
			}

			configuredLabels := strings.Split(v, ",")
			if !collections.MatchItems(qVal, configuredLabels...) {
				continue outer
			}
		}

		return true, nil
	}

	return false, nil
}

type PlaybookSpecEvent struct {
	Class string // canary or component
	Event string // varies depending on the type
}

// map events from `event_queue` to playbook spec events.
var eventToSpecEvent = map[string]PlaybookSpecEvent{
	EventCheckPassed:              {"canary", "passed"},
	EventCheckFailed:              {"canary", "failed"},
	EventComponentStatusHealthy:   {"component", "healthy"},
	EventComponentStatusUnhealthy: {"component", "unhealthy"},
	EventComponentStatusInfo:      {"component", "info"},
	EventComponentStatusWarning:   {"component", "warning"},
	EventComponentStatusError:     {"component", "error"},
}

func onApprovalUpdated(ctx *api.Context, event api.Event) error {
	playbookID := event.Properties["id"]

	var playbook models.Playbook
	if err := ctx.DB().Where("id = ?", playbookID).First(&playbook).Error; err != nil {
		return err
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return err
	}

	if spec.Approval == nil {
		return nil
	}

	return db.UpdatePlaybookRunStatusIfApproved(ctx, playbookID, *spec.Approval)
}

func onPlaybookRunNewApproval(ctx *api.Context, event api.Event) error {
	runID := event.Properties["run_id"]

	var run models.PlaybookRun
	if err := ctx.DB().Where("id = ?", runID).First(&run).Error; err != nil {
		return err
	}

	if run.Status != models.PlaybookRunStatusPending {
		return nil
	}

	var playbook models.Playbook
	if err := ctx.DB().Where("id = ?", run.PlaybookID).First(&playbook).Error; err != nil {
		return err
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return err
	}

	if spec.Approval == nil {
		return nil
	}

	return db.UpdatePlaybookRunStatusIfApproved(ctx, playbook.ID.String(), *spec.Approval)
}
