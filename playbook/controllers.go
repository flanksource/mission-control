package playbook

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/flanksource/commons/utils"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook/actions"
)

type RunResponse struct {
	RunID    string `json:"run_id"`
	StartsAt string `json:"starts_at"`
}

type webhookRequest struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Params  map[string]string `json:"params"`
	Content string            `json:"content"`
	JSON    types.JSONMap     `json:"json"`
}

func newWebhookRequest(c echo.Context) (webhookRequest, error) {
	headers := make(map[string]string)
	for k := range c.Request().Header {
		headers[k] = c.Request().Header.Get(k)
	}
	params := make(map[string]string)
	for k := range c.QueryParams() {
		params[k] = c.QueryParam(k)
	}
	content, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return webhookRequest{}, err
	}

	whr := webhookRequest{
		URL:     c.Request().URL.String(),
		Headers: headers,
		Params:  params,
		Content: string(content),
	}

	if c.Request().Header.Get("Content-Type") == "application/json" {
		if err := json.Unmarshal(content, &whr.JSON); err != nil {
			return webhookRequest{}, err
		}
	}
	return whr, nil
}

type RunParams struct {
	ID          uuid.UUID         `json:"id"`
	ConfigID    uuid.UUID         `json:"config_id"`
	CheckID     uuid.UUID         `json:"check_id"`
	ComponentID uuid.UUID         `json:"component_id"`
	Params      map[string]string `json:"params"`
	Request     webhookRequest    `json:"request"`
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

func paramStr(params []v1.PlaybookParameter) string {
	if len(params) == 0 {
		return " no params expected."
	}

	out := " supported params: "
	for _, p := range params {
		out += fmt.Sprintf("(%s=%s), ", p.Name, p.Label)
	}

	out = out[:len(out)-1]
	return out
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
		r.Params[defaultParams[i].Name] = defaultParams[i].Default
	}
	return nil
}

func (r *RunParams) validateParams(params []v1.PlaybookParameter) error {
	// mandatory params are those that do not have default values
	var mandatoryParams int
	for _, p := range params {
		if p.Default == "" {
			mandatoryParams++
		}
	}

	if len(r.Params) < mandatoryParams {
		return fmt.Errorf("insufficent parameters. expected %d (at least: %d), got %d. %s", len(params), mandatoryParams, len(r.Params), paramStr(params))
	}

	for k := range r.Params {
		var ok bool
		for _, p := range params {
			if k == p.Name {
				ok = true
				break
			}
		}

		if !ok {
			return fmt.Errorf("unknown parameter %s. %s", k, paramStr(params))
		}
	}

	return nil
}

// HandlePlaybookRun handles playbook run requests.
func HandlePlaybookRun(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var req RunParams
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Error: err.Error(), Message: "invalid request"})
	}

	if err := req.valid(); err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Error: err.Error(), Message: "invalid request"})
	}

	playbook, err := db.FindPlaybook(ctx, req.ID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Error: err.Error(), Message: "failed to get playbook"})
	} else if playbook == nil {
		return c.JSON(http.StatusNotFound, dutyAPI.HTTPError{Error: "not found", Message: fmt.Sprintf("playbook(id=%s) not found", req.ID)})
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
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Error: err.Error(), Message: "invalid request"})
	}
	if err := req.valid(); err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Error: err.Error(), Message: "invalid request"})
	}

	playbook, err := db.FindPlaybook(ctx, req.ID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Error: err.Error(), Message: "failed to get playbook"})
	} else if playbook == nil {
		return c.JSON(http.StatusNotFound, dutyAPI.HTTPError{Error: "not found", Message: fmt.Sprintf("playbook(id=%s) not found", req.ID)})
	}

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
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Error: err.Error(), Message: "unable to prepare template env"})
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Error: err.Error(), Message: "failed to unmarshal playbook spec"})
	}

	if err := checkPlaybookFilter(ctx, spec, env); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Error: "Playbook validation failed", Message: err.Error()})
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
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Error: "provide exactly one of: config_id, check_id or component_id", Message: "invalid request"})
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
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Error: err.Error(), Message: "invalid playbook id"})
	}

	runUUID, err := uuid.Parse(runID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Error: err.Error(), Message: "invalid run id"})
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
		return c.JSON(http.StatusNotFound, dutyAPI.HTTPError{Error: "not found", Message: fmt.Sprintf("playbook(webhook_path=%s) not found", path)})
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Error: err.Error(), Message: "playbook has an invalid spec"})
	}

	if err := authenticateWebhook(ctx, c.Request(), spec.On.Webhook.Authentication); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	var runRequest RunParams
	whr, err := newWebhookRequest(c)
	if err != nil {
		return dutyAPI.WriteError(c, fmt.Errorf("error parsing webhook request data: %w", err))
	}

	runRequest.Request = whr
	runRequest.ID = playbook.ID

	if _, err = validateAndSavePlaybookRun(ctx, playbook, runRequest); err != nil {
		return dutyAPI.WriteError(c, fmt.Errorf("failed to save playbook run: %w", err))
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "ok"})
}

func RegisterRoutes(e *echo.Echo) *echo.Group {
	prefix := "playbook"
	playbookGroup := e.Group(fmt.Sprintf("/%s", prefix))
	playbookGroup.GET("/list", HandlePlaybookList)
	playbookGroup.POST("/webhook/:webhook_path", HandleWebhook)
	playbookGroup.POST("/:id/params", HandleGetPlaybookParams)

	runGroup := playbookGroup.Group("/run")
	runGroup.POST("", HandlePlaybookRun)
	runGroup.GET("/:id", HandleGetPlaybookRun)
	runGroup.POST("/approve/:playbook_id/:run_id", HandlePlaybookRunApproval)

	return playbookGroup
}
