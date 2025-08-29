package playbook

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/gomplate/v3"
	"github.com/google/uuid"
	"github.com/patrickmn/go-cache"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/logs"
	"github.com/flanksource/incident-commander/notification"
	"github.com/flanksource/incident-commander/utils"
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
	api.EventConfigChanged:   {"config", "changed"},
	api.EventConfigDeleted:   {"config", "deleted"},
	api.EventConfigHealthy:   {"config", "healthy"},
	api.EventConfigUnhealthy: {"config", "unhealthy"},
	api.EventConfigWarning:   {"config", "warning"},
	api.EventConfigDegraded:  {"config", "degraded"},
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

	events.RegisterSyncHandler(onNewRun, api.EventPlaybookRun)
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

	case api.EventConfigHealthy, api.EventConfigUnhealthy, api.EventConfigWarning, api.EventConfigUnknown, api.EventConfigDegraded:
		if err := ctx.DB().Model(&models.ConfigItem{}).Where("id = ?", event.Properties["id"]).First(&eventResource.Config).Error; err != nil {
			return dutyAPI.Errorf(dutyAPI.ENOTFOUND, "config(id=%s) not found", event.Properties["id"])
		}

	case api.EventConfigCreated, api.EventConfigUpdated, api.EventConfigDeleted:
		if err := ctx.DB().Model(&models.ConfigItem{}).Where("id = ?", event.Properties["id"]).First(&eventResource.Config).Error; err != nil {
			return dutyAPI.Errorf(dutyAPI.ENOTFOUND, "config(id=%s) not found", event.Properties["id"])
		}

	case api.EventConfigChanged:
		if err := ctx.DB().Model(&models.ConfigItem{}).Where("id = ?", event.Properties["config_id"]).First(&eventResource.Config).Error; err != nil {
			return dutyAPI.Errorf(dutyAPI.ENOTFOUND, "config(id=%s) not found", event.Properties["config_id"])
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
			Status:     models.PlaybookRunStatusScheduled,
			Spec:       p.Spec,
		}

		if playbook.Spec.Approval != nil && !playbook.Spec.Approval.Approvers.Empty() {
			run.Status = models.PlaybookRunStatusPendingApproval
		}

		switch specEvent.Class {
		case "canary":
			run.CheckID = &eventResource.Check.ID
			if ok, err := matchResource(ctx, eventResource.Check.Labels, celEnv, playbook.Spec.On.Canary); err != nil {
				logToJobHistory(ctx, p.ID.String(), err.Error())
				continue
			} else if ok {
				if err := ctx.DB().Create(&run).Error; err != nil {
					return err
				}
			}
		case "component":
			run.ComponentID = &eventResource.Component.ID
			if ok, err := matchResource(ctx, eventResource.Component.Labels, celEnv, playbook.Spec.On.Component); err != nil {
				logToJobHistory(ctx, p.ID.String(), err.Error())
				continue
			} else if ok {
				if err := ctx.DB().Create(&run).Error; err != nil {
					return err
				}
			}
		case "config":
			run.ConfigID = &eventResource.Config.ID
			if ok, err := matchResource(ctx, eventResource.Config.Tags, celEnv, playbook.Spec.On.Config); err != nil {
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
func matchResource(ctx context.Context, labels map[string]string, celEnv map[string]any, matchFilters []v1.PlaybookTriggerEvent) (bool, error) {
outer:
	for _, mf := range matchFilters {
		if mf.Filter != "" {
			res, err := ctx.RunTemplate(gomplate.Template{Expression: mf.Filter}, celEnv)
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

type RunInitiatorType string

const (
	RunInitiatorTypeNotification RunInitiatorType = "notification"
	RunInitiatorTypePlaybook     RunInitiatorType = "playbook"
)

func onNewRun(ctx context.Context, event models.Event) error {
	var playbookID = event.Properties["id"]

	// What triggered the run?
	// Must be either a notification, or a playbook run.
	var (
		parentRunID = event.Properties["parent_run_id"]

		notificationID         = event.Properties["notification_id"]
		notificationDispatchID = event.Properties["notification_dispatch_id"]
	)

	if parentRunID == "" && notificationID == "" {
		return fmt.Errorf("invalid event: must have the identity of the trigger. can be a parent run or a notification")
	} else if parentRunID != "" && notificationID != "" {
		return fmt.Errorf("invalid event: cannot have both parent run and notification as the trigger")
	}

	initiatorType := lo.Ternary(parentRunID != "", RunInitiatorTypePlaybook, RunInitiatorTypeNotification)

	var runParam RunParams
	if v, ok := event.Properties["config_id"]; ok {
		runParam.ConfigID = lo.ToPtr(uuid.MustParse(v))
	}
	if v, ok := event.Properties["component_id"]; ok {
		runParam.ComponentID = lo.ToPtr(uuid.MustParse(v))
	}
	if v, ok := event.Properties["check_id"]; ok {
		runParam.CheckID = lo.ToPtr(uuid.MustParse(v))
	}
	if v, ok := event.Properties["parameters"]; ok {
		if err := json.Unmarshal([]byte(v), &runParam.Params); err != nil {
			return fmt.Errorf("invalid parameters: %s", v)
		}
	}

	switch initiatorType {
	case RunInitiatorTypeNotification:
		if parsed, err := uuid.Parse(notificationDispatchID); err != nil {
			return fmt.Errorf("invalid notification dispatch id: %s", notificationDispatchID)
		} else {
			runParam.NotificationSendID = &parsed
		}

		ctx = ctx.WithSubject(notificationID)

	case RunInitiatorTypePlaybook:
		if parsed, err := uuid.Parse(parentRunID); err != nil {
			return fmt.Errorf("invalid parent run id: %s", parentRunID)
		} else {
			runParam.ParentID = &parsed
		}

		var parentPlaybookRun models.PlaybookRun
		if err := ctx.DB().Select("playbook_id").Where("id = ?", parentRunID).First(&parentPlaybookRun).Error; err != nil {
			return fmt.Errorf("failed to get parent playbook run: %w", err)
		}

		ctx = ctx.WithSubject(parentPlaybookRun.PlaybookID.String())
	}

	var playbook models.Playbook
	if err := ctx.DB().Where("id = ?", playbookID).First(&playbook).Error; err != nil {
		return err
	}

	newRun, err := Run(ctx, &playbook, runParam)
	if err != nil {
		if !utils.MatchOopsErrCode(err, dutyAPI.EFORBIDDEN) {
			return err
		}

		// We don't retry on forbidden errors.
		// we resume the trigger (notification or playbook run)
		ctx.Errorf("unable to start playbook run from events triggered by %s: %v", initiatorType, err)
	}

	switch initiatorType {
	case RunInitiatorTypeNotification:
		columnUpdates := map[string]any{}
		if newRun != nil {
			columnUpdates["playbook_run_id"] = newRun.ID.String()
			columnUpdates["status"] = models.NotificationStatusPendingPlaybookCompletion
		} else {
			columnUpdates["error"] = fmt.Sprintf("playbook run failed: %v", err)
			columnUpdates["status"] = models.NotificationStatusError
		}

		if err := ctx.DB().Model(&models.NotificationSendHistory{}).Where("id = ?", notificationDispatchID).UpdateColumns(columnUpdates).Error; err != nil {
			ctx.Errorf("playbook run initiated but failed to update the notification status (%s): %v", notificationDispatchID, err)
		}

		// Attempt fallback on error if applicable
		if newRun == nil {
			notif, err := notification.GetNotification(ctx, notificationID)
			if err != nil {
				return fmt.Errorf("failed to get notification: %w", err)
			}

			if notif.HasFallbackSet() {
				var sendHistory models.NotificationSendHistory
				if err := ctx.DB().Where("id = ?", notificationDispatchID).First(&sendHistory).Error; err != nil {
					return fmt.Errorf("failed to get notification send history: %w", err)
				}

				if err := models.GenerateFallbackAttempt(ctx.DB(), notif.Notification, sendHistory); err != nil {
					return fmt.Errorf("failed to generate fallback attempt: %w", err)
				}
			}
		}

	case RunInitiatorTypePlaybook:
		// If there was a failure to initiate the child playbook run,
		// we need to resume the parent action.
		if newRun == nil {
			var parentRun models.PlaybookRun
			if err := ctx.DB().Where("id = ?", parentRunID).First(&parentRun).Error; err != nil {
				return fmt.Errorf("failed to get parent run: %w", err)
			}

			// Create a record of the failed playbook run.
			// Else, there'll be no trace of this failure and the parent action will keep trying to run the child
			// indefinitely.
			var playbook models.Playbook
			if dbErr := ctx.DB().Where("id = ?", playbookID).First(&playbook).Error; dbErr != nil {
				return fmt.Errorf("failed to get playbook: %w", dbErr)
			} else {
				failedRun := models.PlaybookRun{
					PlaybookID: playbook.ID,
					ParentID:   &parentRun.ID,
					Status:     models.PlaybookRunStatusFailed,
					StartTime:  lo.ToPtr(time.Now()),
					EndTime:    lo.ToPtr(time.Now()),
					Spec:       playbook.Spec,
					Error:      lo.ToPtr(fmt.Sprintf("failed to start playbook due to permissions: %v", err)),
				}

				if err := ctx.DB().Create(&failedRun).Error; err != nil {
					return ctx.Oops().Wrapf(err, "failed to record a failed child playbook run")
				}
			}

			if err := parentRun.ResumeChildrenWaitingAction(ctx.DB()); err != nil {
				return fmt.Errorf("failed to resume children waiting action: %w", err)
			}
		}
	}

	return nil
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

	if run.Status != models.PlaybookRunStatusPendingApproval {
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
