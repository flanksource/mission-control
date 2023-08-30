package events

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/gomplate/v3"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook"
)

type EventResource struct {
	Component *models.Component `json:"component,omitempty"`
	Check     *models.Check     `json:"check,omitempty"`
}

func SavePlaybookRun(ctx *api.Context, event api.Event) error {
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
		if err := ctx.DB().Model(&models.Check{}).Where("id = ?", event.Properties["id"]).First(&eventResource.Check).Error; err != nil {
			return api.Errorf(api.ENOTFOUND, "check(id=%s) not found", event.Properties["id"])
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
			// What should be the component or config id?
			if ok, err := matchResource(eventResource.Check.Labels, map[string]any{"check": eventResource.Check}, playbook.Spec.On.Canary); err != nil {
				logToJobHistory(ctx, p.ID.String(), err.Error())
				continue
			} else if ok {
				if err := ctx.DB().Create(&run).Error; err != nil {
					return err
				}
			}

		case "component":
			run.ComponentID = &eventResource.Component.ID
			if ok, err := matchResource(eventResource.Component.Labels, map[string]any{"component": eventResource.Component}, playbook.Spec.On.Component); err != nil {
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
	for _, mf := range matchFilters {
		var (
			filterPassed     = true
			allLabelsMatched = true
		)

		if mf.Filter != "" {
			filterPassed = false
			res, err := gomplate.RunTemplate(celEnv, gomplate.Template{Expression: mf.Filter})
			if err != nil {
				return false, err
			}

			filterPassed, _ = strconv.ParseBool(res)
		}

		for k, v := range mf.Labels {
			qVal, ok := labels[k]
			if !ok {
				allLabelsMatched = false
				break
			}

			configuredLabels := strings.Split(v, ",")
			if !collections.MatchItems(qVal, configuredLabels...) {
				allLabelsMatched = false
				break
			}
		}

		if filterPassed && allLabelsMatched {
			return true, nil
		}
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
