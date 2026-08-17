package playbook

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
	"github.com/samber/oops"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/clientapi"
	"github.com/flanksource/incident-commander/db"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/playbook/runner"
	"github.com/flanksource/incident-commander/rbac"
	_ "github.com/flanksource/incident-commander/upstream"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /playbook routes")

	prefix := "playbook"
	playbookGroup := e.Group(fmt.Sprintf("/%s", prefix))
	playbookGroup.GET("/list", HandlePlaybookList, rbac.Playbook(policy.ActionRead))
	playbookGroup.POST("/apply", HandlePlaybookApply, rbac.Playbook(policy.ActionUpdate))
	playbookGroup.POST("/webhook/:webhook_path", HandleWebhook)
	playbookGroup.GET("/webhook/:webhook_path", HandleWebhook)

	playbookGroup.POST("/:id/params", HandleGetPlaybookParams)

	playbookGroup.GET("/events", func(c echo.Context) error {
		return c.JSON(http.StatusOK, EventRing.Get())
	}, rbac.Authorization(policy.ObjectMonitor, policy.ActionRead))

	runGroup := playbookGroup.Group("/run")
	runGroup.POST("", HandlePlaybookRun)
	runGroup.GET("/:id/status", HandleGetPlaybookRunStatus, rbac.Playbook(policy.ActionRead))
	runGroup.GET("/:id", HandleGetPlaybookRun, rbac.Playbook(policy.ActionRead))
	runGroup.POST("/approve/:run_id", HandlePlaybookRunApproval)
	runGroup.POST("/cancel/:run_id", HandlePlaybookRunCancel, rbac.Playbook(policy.ActionUpdate))
}

func HandlePlaybookApply(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	var req clientapi.PlaybookApplyRequest
	if err := c.Bind(&req); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request: %v", err))
	}

	response, err := applyPlaybook(ctx, req)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}
	status := http.StatusOK
	if response.Created {
		status = http.StatusCreated
	}
	return c.JSON(status, response)
}

func applyPlaybook(ctx context.Context, req clientapi.PlaybookApplyRequest) (*clientapi.PlaybookApplyResponse, error) {
	if req.Namespace == "" {
		req.Namespace = "default"
	}
	if req.Name == "" {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "playbook name is required")
	}

	spec, err := v1.ParseAndValidatePlaybookSpec(req.Spec)
	if err != nil {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid playbook spec: %v", err)
	}

	var existing []models.Playbook
	if err := ctx.DB().
		Where("namespace = ? AND name = ? AND deleted_at IS NULL", req.Namespace, req.Name).
		Find(&existing).Error; err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	var target *models.Playbook
	if len(existing) == 1 {
		target = &existing[0]
	} else if len(existing) > 1 {
		for i := range existing {
			if existing[i].Category == spec.Category {
				if target != nil {
					return nil, dutyAPI.Errorf(dutyAPI.ECONFLICT, "multiple playbooks match %s/%s in category %q", req.Namespace, req.Name, spec.Category)
				}
				target = &existing[i]
			}
		}
		if target == nil {
			return nil, dutyAPI.Errorf(dutyAPI.ECONFLICT, "multiple playbooks match %s/%s; specify an existing category", req.Namespace, req.Name)
		}
	}

	created := target == nil
	if created {
		target = &models.Playbook{
			ID:        uuid.New(),
			Namespace: req.Namespace,
			Name:      req.Name,
			Source:    models.SourceUI,
		}
	} else if target.Source != models.SourceUI {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "playbook %s/%s was not created through the API and cannot be applied", target.Namespace, target.Name)
	}

	title := spec.Title
	if title == "" {
		title = req.Name
	}
	target.Title = title
	target.Icon = spec.Icon
	target.Description = spec.Description
	target.Category = spec.Category
	target.Spec = types.JSON(req.Spec)
	if err := ctx.DB().Save(target).Error; err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	return &clientapi.PlaybookApplyResponse{
		Playbook: clientapi.Playbook{
			ID:          target.ID,
			Namespace:   target.Namespace,
			Name:        target.Name,
			Title:       target.Title,
			Icon:        target.Icon,
			Description: target.Description,
			Spec:        json.RawMessage(target.Spec),
			Source:      target.Source,
			Category:    target.Category,
			CreatedBy:   target.CreatedBy,
			CreatedAt:   target.CreatedAt,
			UpdatedAt:   target.UpdatedAt,
			DeletedAt:   target.DeletedAt,
		},
		Created: created,
	}, nil
}

type RunResponse struct {
	RunID    string `json:"run_id"`
	StartsAt string `json:"starts_at"`
}

// HandlePlaybookRun handles playbook run requests.
func HandlePlaybookRun(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var req RunParams
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, oops.Wrap(err))
	}

	if err := req.valid(); err != nil {
		return c.JSON(http.StatusBadRequest, oops.Wrapf(err, "invalid request"))
	}

	playbook, err := query.FindPlaybook(ctx, req.ID.String())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, oops.Wrapf(err, "failed to get playbook"))
	} else if playbook == nil {
		return c.JSON(http.StatusNotFound, oops.Errorf("playbook '%s' not found", req.ID))
	}

	run, err := Run(ctx, playbook, req)
	if err != nil {
		return dutyAPI.WriteError(c, oops.Wrap(err))
	}

	return c.JSON(http.StatusCreated, RunResponse{
		RunID:    run.ID.String(),
		StartsAt: run.ScheduledTime.Format(time.RFC3339),
	})
}

type GetParamsResponse struct {
	Params []v1.PlaybookParameter `json:"params"`
}

func HandleGetPlaybookParams(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var req RunParams
	if err := c.Bind(&req); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request: %v", err))
	}
	if err := req.valid(); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request: %v", err))
	}

	playbook, err := query.FindPlaybook(ctx, req.ID.String())
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	} else if playbook == nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "playbook(id=%s) not found", req.ID))
	}

	ctx = ctx.WithNamespace(playbook.Namespace)

	dummyRun := models.PlaybookRun{
		PlaybookID: playbook.ID,
		Spec:       playbook.Spec,
		CreatedBy:  lo.ToPtr(ctx.User().ID),
		Parameters: types.JSONStringMap(req.Params),
	}
	if req.ComponentID != nil {
		dummyRun.ComponentID = req.ComponentID
	}
	if req.ConfigID != nil {
		dummyRun.ConfigID = req.ConfigID
	}
	if req.CheckID != nil {
		dummyRun.CheckID = req.CheckID
	}

	env, err := runner.CreateTemplateEnv(ctx, playbook, dummyRun, nil)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	if !dutyRBAC.HasPermission(ctx, ctx.Subject(), env.ABACAttributes(), policy.ActionRead) {
		return dutyAPI.WriteError(c, ctx.Oops().
			Code(dutyAPI.EFORBIDDEN).
			With("permission", policy.ActionRead, "objects", env.ABACAttributes()).
			Wrap(errors.New("access denied: read access to resource not allowed")))
	}

	if attr, err := dummyRun.GetABACAttributes(ctx.DB()); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	} else if !dutyRBAC.HasPermission(ctx, ctx.Subject(), attr, policy.ActionPlaybookRun) {
		return dutyAPI.WriteError(c, ctx.Oops().
			Code(dutyAPI.EFORBIDDEN).
			With("permission", policy.ActionPlaybookRun, "objects", attr).
			Wrap(fmt.Errorf("access denied to subject(%s): cannot run playbook on this resource", ctx.Subject())))
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	if err := runner.CheckPlaybookFilter(ctx, spec, env); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	templater := ctx.NewStructTemplater(env.AsMap(ctx), "template", nil)
	if err := templater.Walk(&spec.Parameters); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "failed to walk template"))
	}

	return c.JSON(http.StatusOK, GetParamsResponse{
		Params: spec.Parameters,
	})
}

func HandleGetPlaybookRun(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	id := c.Param("id")

	run, err := db.GetPlaybookRun(ctx, id)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, run)
}

func HandleGetPlaybookRunStatus(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	id := c.Param("id")

	runID, err := uuid.Parse(id)
	if err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid run id: %v", err))
	}

	summary, err := GetPlaybookStatus(ctx, runID)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	return c.JSON(http.StatusOK, summary)
}

// Takes config id or component id as a query param
// and returns all the available playbook that supports
// the given component or config.
func HandlePlaybookList(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var (
		configID    = c.QueryParam("config_id")
		checkID     = c.QueryParam("check_id")
		componentID = c.QueryParam("component_id")
	)
	if targetCount(configID, componentID, checkID) > 1 {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "provide at most one of: config_id, check_id or component_id"))
	}

	var playbooks []api.PlaybookListItem
	var err error
	if configID != "" {
		playbooks, err = ListPlaybooksForConfig(ctx, configID)
		if err != nil {
			return dutyAPI.WriteError(c, err)
		}
	} else if componentID != "" {
		playbooks, err = ListPlaybooksForComponent(ctx, componentID)
		if err != nil {
			return dutyAPI.WriteError(c, err)
		}
	} else if checkID != "" {
		playbooks, err = ListPlaybooksForCheck(ctx, checkID)
		if err != nil {
			return dutyAPI.WriteError(c, err)
		}
	} else {
		playbooks, err = db.ListPlaybooks(ctx)
		if err != nil {
			return dutyAPI.WriteError(c, err)
		}
	}

	return c.JSON(http.StatusOK, playbooks)
}

func targetCount(values ...string) int {
	count := 0
	for _, value := range values {
		if value != "" {
			count++
		}
	}
	return count
}

func HandlePlaybookRunCancel(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	runID := c.Param("run_id")
	runUUID, err := uuid.Parse(runID)
	if err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid run id: %v", err))
	}

	run, err := models.PlaybookRun{ID: runUUID}.Load(ctx.DB())
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	} else if run == nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "playbook run(id=%s) not found", runID))
	}

	attr, err := run.GetABACAttributes(ctx.DB())
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	if !dutyRBAC.HasPermission(ctx, ctx.Subject(), attr, policy.ActionPlaybookCancel) {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("you do not have permission to cancel this playbook run"))
	}

	if lo.Contains(models.PlaybookRunStatusFinalStates, run.Status) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "playbook run(id=%s) is in %s state and cannot be cancelled", runID, run.Status))
	}

	if err := run.Cancel(ctx.DB()); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "playbook run cancelled"})
}
