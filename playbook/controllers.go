package playbook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flanksource/commons/utils"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/flanksource/incident-commander/rbac"
)

type RunResponse struct {
	RunID    string `json:"run_id"`
	StartsAt string `json:"starts_at"`
}

type RunParams struct {
	ID          uuid.UUID               `json:"id"`
	ConfigID    uuid.UUID               `json:"config_id"`
	CheckID     uuid.UUID               `json:"check_id"`
	ComponentID uuid.UUID               `json:"component_id"`
	Params      map[string]string       `json:"params"`
	Request     *actions.WebhookRequest `json:"request"`
}

func (r *RunParams) valid() error {
	if r.ID == uuid.Nil {
		return fmt.Errorf("playbook id is required")
	}

	var providedCount int
	if r.ConfigID != uuid.Nil {
		providedCount++
	}
	if r.ComponentID != uuid.Nil {
		providedCount++
	}
	if r.CheckID != uuid.Nil {
		providedCount++
	}

	if providedCount > 1 {
		return fmt.Errorf("provide none or exactly one of config_id, component_id, or check_id")
	}

	return nil
}

func (r *RunParams) setDefaults(ctx context.Context, spec v1.PlaybookSpec, templateEnv actions.TemplateEnv) error {
	if len(spec.Parameters) == len(r.Params) {
		return nil
	}

	defaultParams := []v1.PlaybookParameter{}
	for _, p := range spec.Parameters {
		if _, ok := r.Params[p.Name]; !ok {
			defaultParams = append(defaultParams, p)
		}
	}

	templater := ctx.NewStructTemplater(templateEnv.AsMap(), "template", nil)
	if err := templater.Walk(&defaultParams); err != nil {
		return fmt.Errorf("failed to walk template: %w", err)
	}

	if r.Params == nil {
		r.Params = make(map[string]string)
	}
	for i := range defaultParams {
		r.Params[defaultParams[i].Name] = string(defaultParams[i].Default)
	}
	return nil
}

func (r *RunParams) validateParams(params []v1.PlaybookParameter) error {
	var missingRequiredParams []string
	for _, p := range params {
		if p.Required {
			if _, ok := r.Params[p.Name]; !ok {
				missingRequiredParams = append(missingRequiredParams, p.Name)
			}
		}
	}

	if len(missingRequiredParams) != 0 {
		return fmt.Errorf("missing required parameter(s): %s", strings.Join(missingRequiredParams, ","))
	}

	unknownParams, _ := lo.Difference(
		lo.MapToSlice(r.Params, func(k string, _ string) string { return k }),
		lo.Map(params, func(v v1.PlaybookParameter, _ int) string { return v.Name }),
	)

	if len(unknownParams) != 0 {
		return fmt.Errorf("unknown parameter(s): %s", strings.Join(unknownParams, ", "))
	}

	return nil
}

// HandlePlaybookRun handles playbook run requests.
func HandlePlaybookRun(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var req RunParams
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Err: err.Error(), Message: "invalid request"})
	}

	if err := req.valid(); err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Err: err.Error(), Message: "invalid request"})
	}

	playbook, err := db.FindPlaybook(ctx, req.ID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Err: err.Error(), Message: "failed to get playbook"})
	} else if playbook == nil {
		return c.JSON(http.StatusNotFound, dutyAPI.HTTPError{Err: "not found", Message: fmt.Sprintf("playbook(id=%s) not found", req.ID)})
	}

	run, err := validateAndSavePlaybookRun(ctx, playbook, req)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusCreated, RunResponse{
		RunID:    run.ID.String(),
		StartsAt: run.ScheduledTime.Format(time.RFC3339),
	})
}

func HandleGetPlaybookParams(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var req RunParams
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Err: err.Error(), Message: "invalid request"})
	}
	if err := req.valid(); err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Err: err.Error(), Message: "invalid request"})
	}

	playbook, err := db.FindPlaybook(ctx, req.ID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Err: err.Error(), Message: "failed to get playbook"})
	} else if playbook == nil {
		return c.JSON(http.StatusNotFound, dutyAPI.HTTPError{Err: "not found", Message: fmt.Sprintf("playbook(id=%s) not found", req.ID)})
	}

	ctx = ctx.WithNamespace(playbook.Namespace)

	dummyRun := models.PlaybookRun{
		PlaybookID: playbook.ID,
		CreatedBy:  lo.ToPtr(ctx.User().ID),
	}
	if req.ComponentID != uuid.Nil {
		dummyRun.ComponentID = &req.ComponentID
	}
	if req.ConfigID != uuid.Nil {
		dummyRun.ConfigID = &req.ConfigID
	}
	if req.CheckID != uuid.Nil {
		dummyRun.CheckID = &req.CheckID
	}

	env, err := prepareTemplateEnv(ctx, *playbook, dummyRun)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Err: err.Error(), Message: "unable to prepare template env"})
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Err: err.Error(), Message: "failed to unmarshal playbook spec"})
	}

	if err := checkPlaybookFilter(ctx, spec, env); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Err: "Playbook validation failed", Message: err.Error()})
	}

	templater := ctx.NewStructTemplater(env.AsMap(), "template", nil)
	if err := templater.Walk(&spec.Parameters); err != nil {
		return fmt.Errorf("failed to walk template: %w", err)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"params": spec.Parameters,
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

	if configID == "" && componentID == "" && checkID == "" {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Err: "provide exactly one of: config_id, check_id or component_id", Message: "invalid request"})
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
	}

	return c.JSON(http.StatusOK, playbooks)
}

func HandlePlaybookRunApproval(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var (
		playbookID = c.Param("playbook_id")
		runID      = c.Param("run_id")
	)

	playbookUUID, err := uuid.Parse(playbookID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Err: err.Error(), Message: "invalid playbook id"})
	}

	runUUID, err := uuid.Parse(runID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Err: err.Error(), Message: "invalid run id"})
	}

	if err := ApproveRun(ctx, playbookUUID, runUUID); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "playbook run approved"})
}

func HandleWebhook(c echo.Context) error {
	ctx := c.Request().Context().(context.Context).WithUser(&models.Person{ID: utils.Deref(api.SystemUserID)})

	var path = c.Param("webhook_path")
	playbook, err := db.FindPlaybookByWebhookPath(ctx, path)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	} else if playbook == nil {
		return c.JSON(http.StatusNotFound, dutyAPI.HTTPError{Err: "not found", Message: fmt.Sprintf("playbook(webhook_path=%s) not found", path)})
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Err: err.Error(), Message: "playbook has an invalid spec"})
	}

	if err := authenticateWebhook(ctx, c.Request(), spec.On.Webhook.Authentication); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	var runRequest RunParams
	whr, err := actions.NewWebhookRequest(c)
	if err != nil {
		return dutyAPI.WriteError(c, fmt.Errorf("error parsing webhook request data: %w", err))
	}

	runRequest.ID = playbook.ID
	runRequest.Request = whr

	if _, err = validateAndSavePlaybookRun(ctx, playbook, runRequest); err != nil {
		return dutyAPI.WriteError(c, fmt.Errorf("failed to save playbook run: %w", err))
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "ok"})
}

func RegisterRoutes(e *echo.Echo) *echo.Group {
	prefix := "playbook"
	playbookGroup := e.Group(fmt.Sprintf("/%s", prefix))
	playbookGroup.GET("/list", HandlePlaybookList, rbac.Playbook(rbac.ActionRead))
	playbookGroup.POST("/webhook/:webhook_path", HandleWebhook, rbac.Playbook(rbac.ActionRun))
	playbookGroup.POST("/:id/params", HandleGetPlaybookParams, rbac.Playbook(rbac.ActionRun))

	runGroup := playbookGroup.Group("/run")
	runGroup.POST("", HandlePlaybookRun, rbac.Playbook(rbac.ActionRun))
	runGroup.GET("/:id", HandleGetPlaybookRun, rbac.Playbook(rbac.ActionRead))
	runGroup.POST("/approve/:playbook_id/:run_id", HandlePlaybookRunApproval, rbac.Playbook(rbac.ActionApprove))

	return playbookGroup
}
