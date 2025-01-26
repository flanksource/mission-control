package playbook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
	"github.com/samber/oops"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/playbook/runner"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/policy"
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
	playbookGroup.POST("/webhook/:webhook_path", HandleWebhook)
	playbookGroup.POST("/:id/params", HandleGetPlaybookParams, rbac.Playbook(policy.ActionPlaybookRun))

	playbookGroup.GET("/events", func(c echo.Context) error {
		return c.JSON(http.StatusOK, EventRing.Get())
	}, rbac.Authorization(policy.ObjectMonitor, policy.ActionRead))

	runGroup := playbookGroup.Group("/run")
	runGroup.POST("", HandlePlaybookRun)
	runGroup.GET("/:id", HandleGetPlaybookRun, rbac.Playbook(policy.ActionRead))
	runGroup.POST("/approve/:run_id", HandlePlaybookRunApproval)
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
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Err: err.Error(), Message: "invalid request"})
	}
	if err := req.valid(); err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Err: err.Error(), Message: "invalid request"})
	}

	playbook, err := query.FindPlaybook(ctx, req.ID.String())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Err: err.Error(), Message: "failed to get playbook"})
	} else if playbook == nil {
		return c.JSON(http.StatusNotFound, dutyAPI.HTTPError{Err: "not found", Message: fmt.Sprintf("playbook(id=%s) not found", req.ID)})
	}

	ctx = ctx.WithNamespace(playbook.Namespace)

	dummyRun := models.PlaybookRun{
		PlaybookID: playbook.ID,
		Spec:       playbook.Spec,
		CreatedBy:  lo.ToPtr(ctx.User().ID),
		Parameters: req.Params,
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

	env, err := runner.CreateTemplateEnv(ctx, playbook, &dummyRun, nil)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Err: err.Error(), Message: "unable to prepare template env"})
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Err: err.Error(), Message: "failed to unmarshal playbook spec"})
	}

	if err := runner.CheckPlaybookFilter(ctx, spec, env); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Err: "Playbook validation failed", Message: err.Error()})
	}

	templater := ctx.NewStructTemplater(env.AsMap(ctx), "template", nil)
	if err := templater.Walk(&spec.Parameters); err != nil {
		return ctx.Oops().Wrapf(err, "failed to walk template")
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
