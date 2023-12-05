package playbook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/utils"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type RunResponse struct {
	RunID    string `json:"run_id"`
	StartsAt string `json:"starts_at"`
}

type RunParams struct {
	ID          uuid.UUID         `json:"id"`
	ConfigID    uuid.UUID         `json:"config_id"`
	CheckID     uuid.UUID         `json:"check_id"`
	ComponentID uuid.UUID         `json:"component_id"`
	Params      map[string]string `json:"params"`
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

	if providedCount != 1 {
		return fmt.Errorf("provide exactly one of config_id, component_id, or check_id")
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

func (r *RunParams) validateParams(params []v1.PlaybookParameter) error {
	if len(params) != len(r.Params) {
		return fmt.Errorf("invalid number of parameters. expected %d, got %d.%s", len(params), len(r.Params), paramStr(params))
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
			return fmt.Errorf("unknown parameter %s.%s", k, paramStr(params))
		}
	}

	return nil
}

// HandlePlaybookRun handles playbook run requests.
func HandlePlaybookRun(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var req RunParams
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error(), Message: "invalid request"})
	}

	if err := req.valid(); err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error(), Message: "invalid request"})
	}

	playbook, err := db.FindPlaybook(ctx, req.ID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to get playbook"})
	} else if playbook == nil {
		return c.JSON(http.StatusNotFound, api.HTTPError{Error: "not found", Message: fmt.Sprintf("playbook(id=%s) not found", req.ID)})
	}

	run, err := validateAndSavePlaybook(ctx, playbook, req)
	if err != nil {
		return api.WriteError(c, err)
	}

	return c.JSON(http.StatusCreated, RunResponse{
		RunID:    run.ID.String(),
		StartsAt: run.StartTime.Format(time.RFC3339),
	})
}

func HandleGetPlaybookRun(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	id := c.Param("id")

	run, err := db.GetPlaybookRun(ctx, id)
	if err != nil {
		return api.WriteError(c, err)
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
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: "provide exactly one of: config_id, check_id or component_id", Message: "invalid request"})
	}

	var playbooks []api.PlaybookListItem
	var err error
	if configID != "" {
		playbooks, err = ListPlaybooksForConfig(ctx, configID)
		if err != nil {
			return api.WriteError(c, err)
		}
	} else if componentID != "" {
		playbooks, err = ListPlaybooksForComponent(ctx, componentID)
		if err != nil {
			return api.WriteError(c, err)
		}
	} else if checkID != "" {
		playbooks, err = ListPlaybooksForCheck(ctx, checkID)
		if err != nil {
			return api.WriteError(c, err)
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
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error(), Message: "invalid playbook id"})
	}

	runUUID, err := uuid.Parse(runID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error(), Message: "invalid run id"})
	}

	if err := ApproveRun(ctx, playbookUUID, runUUID); err != nil {
		return api.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, api.HTTPSuccess{Message: "playbook run approved"})
}

func HandleWebhook(c echo.Context) error {
	ctx := c.Request().Context().(context.Context).WithUser(&models.Person{ID: utils.Deref(api.SystemUserID)})

	var path = c.Param("webhook_path")
	playbook, err := db.FindPlaybookByWebhookPath(ctx, path)
	if err != nil {
		return api.WriteError(c, err)
	} else if playbook == nil {
		return c.JSON(http.StatusNotFound, api.HTTPError{Error: "not found", Message: fmt.Sprintf("playbook(webhook_path=%s) not found", path)})
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "playbook has an invalid spec"})
	}

	if err := authenticateWebhook(ctx, c.Request(), spec.On.Webhook.Authentication); err != nil {
		return api.WriteError(c, err)
	}

	runRequest := RunParams{
		ID: playbook.ID,
	}

	if _, err = validateAndSavePlaybook(ctx, playbook, runRequest); err != nil {
		logger.Errorf("failed to save playbook run: %v", err)
	}

	return c.JSON(http.StatusOK, api.HTTPSuccess{Message: "ok"})
}

func RegisterRoutes(e *echo.Echo, prefix string) *echo.Group {
	playbookGroup := e.Group(fmt.Sprintf("/%s", prefix))
	playbookGroup.GET("/list", HandlePlaybookList)
	playbookGroup.POST("/webhook/:webhook_path", HandleWebhook)

	runGroup := playbookGroup.Group("/run")
	runGroup.POST("", HandlePlaybookRun)
	runGroup.GET("/:id", HandleGetPlaybookRun)
	runGroup.POST("/approve/:playbook_id/:run_id", HandlePlaybookRunApproval)

	return playbookGroup
}
