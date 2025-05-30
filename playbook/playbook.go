package playbook

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/duration"
	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/samber/oops"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook/runner"
	"github.com/flanksource/incident-commander/vars"
)

type PlaybookSummary struct {
	Playbook models.Playbook            `json:"playbook,omitempty"`
	Run      models.PlaybookRun         `json:"run,omitempty"`
	Actions  []models.PlaybookRunAction `json:"actions,omitempty"`
}

func GetPlaybookStatus(ctx context.Context, runId uuid.UUID) (PlaybookSummary, error) {
	summary := PlaybookSummary{}
	run, err := models.PlaybookRun{ID: runId}.Load(ctx.DB())
	if err != nil {
		return summary, err
	} else {
		summary.Run = *run
	}

	playbook, err := models.Playbook{ID: run.PlaybookID}.Load(ctx.DB())
	if err != nil {
		return summary, err
	} else {
		summary.Playbook = *playbook
	}

	actions, err := run.GetActions(ctx.DB())
	if err != nil {
		return summary, err
	} else {
		summary.Actions = actions
	}

	return summary, nil
}

func CreateOrSaveFromFile(ctx context.Context, file string) (*models.Playbook, error) {
	var spec v1.Playbook

	manifest, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	// We can get multiple yamls, we choose first non empty, else throw an error
	allManifests := strings.Split(string(manifest), "---")
	for _, m := range allManifests {
		err = yamlutil.Unmarshal([]byte(m), &spec)
		if err != nil {
			return nil, err
		}

		if spec.Name == "" {
			logger.Infof("[Skipping] Got empty name for spec %s", m)
			continue
		}
	}
	if spec.Name == "" {
		return nil, fmt.Errorf("Error no name in spec %v", spec)
	}

	return db.SavePlaybook(ctx, &spec)
}

// validateAndSavePlaybookRun creates and saves a run from a run request after validating the run parameters.
func Run(ctx context.Context, playbook *models.Playbook, req RunParams) (*models.PlaybookRun, error) {
	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return nil, err
	}

	if err := req.Params.Sanitize(ctx, spec.Parameters); err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to sanitize parameters")
	}

	ctx = ctx.WithObject(playbook, req).WithLoggingValues("req", req)
	ctx.Infof("running \n%v\n", logger.Pretty(req))

	run := models.PlaybookRun{
		PlaybookID:         playbook.ID,
		Status:             models.PlaybookRunStatusScheduled,
		AgentID:            req.AgentID,
		Parameters:         types.JSONStringMap(req.Params),
		NotificationSendID: req.NotificationSendID,
		ParentID:           req.ParentID,
	}

	// The run gets its own copy of the spec and uses that throughout its lifecycle.
	// Any change to the playbook spec while the run is in progress should not affect the run.
	run.Spec = playbook.Spec

	if ctx.User() != nil {
		run.CreatedBy = &ctx.User().ID
	}

	if spec.Approval != nil && !spec.Approval.Approvers.Empty() {
		run.Status = models.PlaybookRunStatusPendingApproval
	}

	if spec.Timeout == "" {
		run.Timeout = ctx.Properties().Duration("playbook.run.timeout", vars.PlaybookRunTimeout)
	} else {
		timeout, err := duration.ParseDuration(spec.Timeout)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "invalid timeout duration (%s)", spec.Timeout)
		}

		run.Timeout = time.Duration(timeout)
	}

	if req.ComponentID != nil {
		run.ComponentID = req.ComponentID
	}

	if req.ConfigID != nil {
		run.ConfigID = req.ConfigID
	}

	if req.CheckID != nil {
		run.CheckID = req.CheckID
	}

	if req.Request != nil {
		whr, err := collections.StructToJSON(req.Request)
		if err != nil {
			return nil, ctx.Oops().Wrap(err)
		}

		var whrMap map[string]any
		if err := json.Unmarshal([]byte(whr), &whrMap); err != nil {
			return nil, ctx.Oops().Wrap(err)
		}
		run.Request = whrMap
	}

	templateEnv, err := runner.CreateTemplateEnv(ctx, playbook, run, nil)
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	// Must have read access on the resource (required to prevent guests from accessing unauthorized resources)
	if !rbac.HasPermission(ctx, ctx.Subject(), templateEnv.ABACAttributes(), policy.ActionRead) {
		return nil, ctx.Oops().
			Code(dutyAPI.EFORBIDDEN).
			With("permission", policy.ActionRead, "objects", templateEnv.ABACAttributes()).
			Wrap(fmt.Errorf("access denied: read access to resource not allowed for subject: %s", ctx.Subject()))
	}

	if attr, err := run.GetABACAttributes(ctx.DB()); err != nil {
		return nil, ctx.Oops().Wrap(err)
	} else if !rbac.HasPermission(ctx, ctx.Subject(), attr, policy.ActionPlaybookRun) {
		return nil, ctx.Oops().
			Code(dutyAPI.EFORBIDDEN).
			With("permission", policy.ActionPlaybookRun, "objects", attr).
			Wrap(fmt.Errorf("access denied to subject(%s): cannot run playbook on this resource", ctx.Subject()))
	}

	// Rest of the playbook must run using the playbook's permission.
	ctx = ctx.WithSubject(playbook.ID.String())

	if err := req.setDefaults(ctx, spec, templateEnv); err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	if err := req.validateParams(spec.Parameters); err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	run.Parameters = types.JSONStringMap{}
	for k, v := range req.Params {
		run.Parameters[k] = fmt.Sprintf("%v", v)
	}

	if err := runner.CheckPlaybookFilter(ctx, spec, templateEnv); err != nil {
		return nil, err
	}

	var runSpec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &runSpec); err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to unmarshal playbook spec")
	}

	{
		// If an exec action uses connection from the config item, and an agent isn't assigned to the action,
		// we use the config's agent
		for i, action := range runSpec.Actions {
			hasExplicitAgent := len(runSpec.RunsOn) != 0 || len(action.RunsOn) != 0
			usesFromConfigItem := action.Exec != nil && action.Exec.Connections.FromConfigItem != nil
			if !hasExplicitAgent && usesFromConfigItem {
				templater := ctx.NewStructTemplater(templateEnv.AsMap(ctx), "", nil)
				output, err := templater.Template(*action.Exec.Connections.FromConfigItem)
				if err != nil {
					return nil, ctx.Oops().Wrapf(err, "failed to template config item %s", *action.Exec.Connections.FromConfigItem)
				}

				var fromConfigItem models.ConfigItem
				if err := ctx.DB().Select("id", "agent_id").Where("id = ?", output).Find(&fromConfigItem).Error; err != nil {
					return nil, ctx.Oops().Wrapf(err, "failed to find config item %s", output)
				} else if fromConfigItem.AgentID != uuid.Nil {
					runSpec.Actions[i].RunsOn = []string{fromConfigItem.AgentID.String()}
				}
			}
		}

		// Template run's spec (runsOn)
		var runsOn []string
		for _, specRunOn := range runSpec.RunsOn {
			templater := ctx.NewStructTemplater(templateEnv.AsMap(ctx), "", nil)
			output, err := templater.Template(specRunOn)
			if err != nil {
				return nil, ctx.Oops().Wrapf(err, "failed to template run's spec %s", specRunOn)
			}
			runsOn = append(runsOn, output)
		}

		runSpec.RunsOn = runsOn
	}

	run.Spec, err = json.Marshal(runSpec)
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	if err := savePlaybookRun(ctx, &run); err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to create playbook run")
	}

	if err := saveRunAsConfigChange(ctx, playbook, run, req.Params); err != nil {
		ctx.Logger.Errorf("failed to save playbook run as config change: %v", err)
	}

	return &run, nil
}

func saveRunAsConfigChange(ctx context.Context, playbook *models.Playbook, run models.PlaybookRun, parameters any) error {
	if run.ConfigID == nil {
		return nil
	}

	change := models.ConfigChange{
		ExternalChangeID: lo.ToPtr(uuid.NewString()),
		Severity:         models.SeverityInfo,
		ConfigID:         run.ConfigID.String(),
		ChangeType:       fmt.Sprintf("Playbook%s", cases.Title(language.English).String(string(run.Status))),
		Source:           "Playbook",
		Summary:          fmt.Sprintf("Playbook: %s", playbook.Name),
	}

	switch run.Status {
	case models.PlaybookRunStatusScheduled:
		change.Severity = models.SeverityInfo
		change.ChangeType = "PlaybookScheduled"
		change.ExternalChangeID = lo.ToPtr(run.ID.String())

		details := map[string]any{
			"parameters": parameters,
			"spec":       run.Spec,
		}
		detailsJSON, err := json.Marshal(details)
		if err != nil {
			return fmt.Errorf("error marshaling playbook details into config changes: %w", err)
		}
		change.Details = detailsJSON

	case models.PlaybookRunStatusRunning:
		change.ChangeType = "PlaybookStarted"
		change.Severity = models.SeverityInfo

	case models.PlaybookRunStatusCompleted:
		change.ChangeType = "PlaybookCompleted"
		change.Severity = models.SeverityLow

	case models.PlaybookRunStatusFailed:
		change.Severity = models.SeverityHigh
		change.ChangeType = "PlaybookFailed"
	}

	return ctx.DB().Create(&change).Error
}

// savePlaybookRun saves the run and attempts register an approval from the caller.
func savePlaybookRun(ctx context.Context, run *models.PlaybookRun) error {
	tx := ctx.DB().Begin()
	if tx.Error != nil {
		return ctx.Oops("db").Wrap(tx.Error)
	}
	defer tx.Rollback()

	ctx = ctx.WithDB(tx, ctx.Pool())
	if err := ctx.DB().Create(run).Error; err != nil {
		return ctx.Oops("db").Wrap(err)
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(run.Spec, &spec); err != nil {
		return ctx.Oops().Wrap(err)
	}

	if requiresApproval(spec) {
		// Attempt to auto approve run
		if err := ApproveRun(ctx, run.ID); err != nil {
			if oopserr, ok := oops.AsOops(err); ok {
				switch oopserr.Code() {
				case dutyAPI.EFORBIDDEN, dutyAPI.EINVALID:
					// ignore these errors
				default:
					return ctx.Oops().Errorf("error while attempting to auto approve run: %w", err)
				}
			}
		}
	}

	return tx.Commit().Error
}

func ListPlaybooksForConfig(ctx context.Context, id string) ([]api.PlaybookListItem, error) {
	var config models.ConfigItem
	if err := ctx.DB().Where("id = ?", id).Find(&config).Error; err != nil {
		return nil, ctx.Oops("db").Wrap(err)
	} else if config.ID == uuid.Nil {
		return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("config(id=%s) not found", id)
	}

	list, _, err := db.FindPlaybooksForConfig(ctx, config)
	return list, ctx.Oops().Wrap(err)
}

func ListPlaybooksForComponent(ctx context.Context, id string) ([]api.PlaybookListItem, error) {
	var component models.Component
	if err := ctx.DB().Where("id = ?", id).Find(&component).Error; err != nil {
		return nil, ctx.Oops("db").Wrap(err)
	} else if component.ID == uuid.Nil {
		return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("component '%s' not found", id)
	}

	list, _, err := db.FindPlaybooksForComponent(ctx, component)
	return list, ctx.Oops().Wrap(err)
}

func ListPlaybooksForCheck(ctx context.Context, id string) ([]api.PlaybookListItem, error) {
	var check models.Check
	if err := ctx.DB().Where("id = ?", id).Find(&check).Error; err != nil {
		return nil, ctx.Oops("db").Wrap(err)
	} else if check.ID == uuid.Nil {
		return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("check(id=%s) not found", id)
	}

	list, _, err := db.FindPlaybooksForCheck(ctx, check)
	return list, ctx.Oops().Wrap(err)
}
