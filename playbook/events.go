package playbook

import (
	"fmt"
	"time"

	"encoding/json"
	"strconv"
	"strings"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/gomplate/v3"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/logs"
	"github.com/patrickmn/go-cache"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

type PlaybookSpecEvent struct {
	Class string // canary or component
	Event string // varies depending on the type
}

// map api.from `event_queue` to playbook spec api.
var eventToSpecEvent = map[string]PlaybookSpecEvent{
	api.EventCheckPassed: {"canary", "passed"},
	api.EventCheckFailed: {"canary", "failed"},

	api.EventConfigCreated:   {"config", "created"},
	api.EventConfigUpdated:   {"config", "updated"},
	api.EventConfigDeleted:   {"config", "deleted"},
	api.EventConfigHealthy:   {"config", "healthy"},
	api.EventConfigUnhealthy: {"config", "unhealthy"},
	api.EventConfigWarning:   {"config", "warning"},
	api.EventConfigUnknown:   {"config", "unknown"},

	api.EventComponentHealthy:   {"component", "healthy"},
	api.EventComponentUnhealthy: {"component", "unhealthy"},
	api.EventComponentWarning:   {"component", "warning"},
	api.EventComponentUnknown:   {"component", "unknown"},
}

var (
	eventPlaybooksCache = cache.New(time.Hour*1, time.Hour*1)

	EventRing *events.EventRing
)

func eventPlaybookCacheKey(eventClass, event string) string {
	return fmt.Sprintf("%s::%s", eventClass, event)
}

func init() {
	events.Register(RegisterEvents)
}

func RegisterEvents(ctx context.Context) {
	EventRing = events.NewEventRing(ctx.Properties().Int("events.audit.size", events.DefaultEventLogSize))
	nh := playbookScheduler{Ring: EventRing}
	events.RegisterSyncHandler(nh.Handle, api.EventStatusGroup...)

	events.RegisterSyncHandler(onApprovalUpdated, api.EventPlaybookSpecApprovalUpdated)
	events.RegisterSyncHandler(onPlaybookRunNewApproval, api.EventPlaybookApprovalInserted)

	go func() {
		logs.IfError(StartPlaybookConsumers(ctx), "error starting playbook run consumer")
	}()

	go ListenPlaybookPGNotify(ctx)
}

type EventResource struct {
	Component    *models.Component    `json:"component,omitempty"`
	Config       *models.ConfigItem   `json:"config,omitempty"`
	Check        *models.Check        `json:"check,omitempty"`
	CheckSummary *models.CheckSummary `json:"check_summary,omitempty"`
	Canary       *models.Canary       `json:"canary,omitempty"`
}

func (t *EventResource) AsMap() map[string]any {
	output := map[string]any{}

	if t.Component != nil {
		output["component"] = t.Component.AsMap()
	}
	if t.Config != nil {
		output["config"] = t.Config.AsMap()
	}
	if t.Check != nil {
		output["check"] = t.Check.AsMap()
	}
	if t.Canary != nil {
		output["canary"] = t.Canary.AsMap()
	}
	if t.CheckSummary != nil {
		output["check_summary"] = t.CheckSummary.AsMap()
	}

	return output
}

type playbookScheduler struct {
	Ring *events.EventRing
}

func (t *playbookScheduler) Handle(ctx context.Context, event models.Event) error {
	specEvent, ok := eventToSpecEvent[event.Name]
	if !ok {
		return nil
	}

	playbooks, err := FindPlaybooksForEvent(ctx, specEvent.Class, specEvent.Event)
	if err != nil {
		return fmt.Errorf("error fetching playbooks: %w", err)
	}

	if len(playbooks) == 0 {
		return nil
	}

	var eventResource EventResource
	switch event.Name {
	case api.EventCheckFailed, api.EventCheckPassed:
		checkID := event.Properties["id"]
		if err := ctx.DB().Where("id = ?", checkID).First(&eventResource.Check).Error; err != nil {
			return dutyAPI.Errorf(dutyAPI.ENOTFOUND, "check(id=%s) not found", checkID)
		}

		if summary, err := duty.CheckSummary(ctx, checkID); err != nil {
			return err
		} else if summary != nil {
			eventResource.CheckSummary = summary
		}

		if err := ctx.DB().Where("id = ?", eventResource.Check.CanaryID).First(&eventResource.Canary).Error; err != nil {
			return dutyAPI.Errorf(dutyAPI.ENOTFOUND, "canary(id=%s) not found", eventResource.Check.CanaryID)
		}

	case api.EventComponentHealthy, api.EventComponentUnhealthy, api.EventComponentWarning, api.EventComponentUnknown:
		if err := ctx.DB().Model(&models.Component{}).Where("id = ?", event.Properties["id"]).First(&eventResource.Component).Error; err != nil {
			return dutyAPI.Errorf(dutyAPI.ENOTFOUND, "component(id=%s) not found", event.Properties["id"])
		}

	case api.EventConfigHealthy, api.EventConfigUnhealthy, api.EventConfigWarning, api.EventConfigUnknown:
		if err := ctx.DB().Model(&models.ConfigItem{}).Where("id = ?", event.Properties["id"]).First(&eventResource.Config).Error; err != nil {
			return dutyAPI.Errorf(dutyAPI.ENOTFOUND, "config(id=%s) not found", event.Properties["id"])
		}

	case api.EventConfigCreated, api.EventConfigUpdated, api.EventConfigDeleted:
		if err := ctx.DB().Model(&models.ConfigItem{}).Where("id = ?", event.Properties["id"]).First(&eventResource.Config).Error; err != nil {
			return dutyAPI.Errorf(dutyAPI.ENOTFOUND, "config(id=%s) not found", event.Properties["id"])
		}
	}

	for _, p := range playbooks {
		celEnv := eventResource.AsMap()
		t.Ring.Add(event, celEnv)

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
			if ok, err := matchResource(eventResource.Check.Labels, celEnv, playbook.Spec.On.Canary); err != nil {
				logToJobHistory(ctx, p.ID.String(), err.Error())
				continue
			} else if ok {
				if err := ctx.DB().Create(&run).Error; err != nil {
					return err
				}
			}
		case "component":
			run.ComponentID = &eventResource.Component.ID
			if ok, err := matchResource(eventResource.Component.Labels, celEnv, playbook.Spec.On.Component); err != nil {
				logToJobHistory(ctx, p.ID.String(), err.Error())
				continue
			} else if ok {
				if err := ctx.DB().Create(&run).Error; err != nil {
					return err
				}
			}
		case "config":
			run.ConfigID = &eventResource.Config.ID
			if ok, err := matchResource(eventResource.Config.Tags, celEnv, playbook.Spec.On.Config); err != nil {
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
func logToJobHistory(ctx context.Context, playbookID, err string) {
	jobHistory := models.NewJobHistory(ctx.Logger, "SavePlaybookRun", "playbook", playbookID)
	jobHistory.Start()
	jobHistory.AddError(err)

	if err := jobHistory.End().Persist(ctx.DB()); err != nil {
		logger.Errorf("error persisting job history: %v", err)
	}
}

// matchResource returns true if any one of the matchFilter is true
// for the given labels and cel env.
func matchResource(labels map[string]string, celEnv map[string]any, matchFilters []v1.PlaybookTriggerEvent) (bool, error) {
outer:
	for _, mf := range matchFilters {
		if mf.Filter != "" {
			res, err := gomplate.RunTemplate(celEnv, gomplate.Template{Expression: mf.Filter})
			if err != nil {
				return false, err
			}

			if ok, err := strconv.ParseBool(res); err != nil {
				return false, dutyAPI.Errorf(dutyAPI.EINVALID, "expression (%s) didn't evaluate to a boolean value. got %s", mf.Filter, res)
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

func onApprovalUpdated(ctx context.Context, event models.Event) error {
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

func onPlaybookRunNewApproval(ctx context.Context, event models.Event) error {
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

func FindPlaybooksForEvent(ctx context.Context, eventClass, event string) ([]models.Playbook, error) {
	if playbooks, found := eventPlaybooksCache.Get(eventPlaybookCacheKey(eventClass, event)); found {
		return playbooks.([]models.Playbook), nil
	}

	playbooks, err := db.FindPlaybooksForEvent(ctx, eventClass, event)
	if err != nil {
		return nil, err
	}

	eventPlaybooksCache.SetDefault(eventPlaybookCacheKey(eventClass, event), playbooks)
	return playbooks, nil
}

func clearEventPlaybookCache() {
	eventPlaybooksCache.Flush()
}
