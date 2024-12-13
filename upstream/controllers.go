package upstream

import (
	"fmt"
	"net/http"
	"time"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/upstream"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"
	"go.opentelemetry.io/otel/attribute"

	"github.com/flanksource/incident-commander/artifacts"
	"github.com/flanksource/incident-commander/db"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/playbook/runner"
	"github.com/flanksource/incident-commander/push"
	"github.com/flanksource/incident-commander/rbac"
)

var agentCache = cache.New(3*24*time.Hour, 12*time.Hour)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /upstream routes")

	e.POST("/push/topology", push.PushTopology, rbac.Topology(rbac.ActionUpdate))

	upstreamGroup := e.Group(
		"/upstream",
		rbac.Authorization(rbac.ObjectAgentPush, rbac.ActionUpdate),
		upstream.AgentAuthMiddleware(agentCache),
	)
	upstreamGroup.GET("/ping", upstream.PingHandler)
	upstreamGroup.POST("/push", upstream.NewPushHandler(upstream.NewStatusRingStore(job.EvictedJobs)))
	upstreamGroup.DELETE("/push", upstream.DeleteHandler)

	upstreamGroup.GET("/canary/pull", PullCanaries)
	upstreamGroup.GET("/scrapeconfig/pull", PullScrapeConfigs)

	upstreamGroup.POST("/artifacts/:id", artifactsPushHandler)

	upstreamGroup.GET("/playbook-action", handlePlaybookActionRequest)
}

// handlePlaybookActionRequest returns a playbook action for the agent to run.
func handlePlaybookActionRequest(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	agent := ctx.Agent()
	response, err := runner.GetActionForAgentWithWait(ctx, agent)
	if err != nil {
		logger.Warnf("failed to get action for agent: %+v", err)
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}

// PullCanaries returns all canaries for the  agent
func PullCanaries(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	agent := ctx.Agent()

	var since time.Time
	var err error
	if sinceRaw := c.QueryParam("since"); sinceRaw != "" {
		since, err = time.Parse(time.RFC3339, sinceRaw)
		if err != nil {
			return c.JSON(
				http.StatusBadRequest,
				dutyAPI.HTTPError{Err: fmt.Sprintf("'since' param needs to be a valid RFC3339 timestamp: %v", err)},
			)
		}

		ctx.GetSpan().SetAttributes(attribute.String("upstream.pull.canaries.since", sinceRaw))
	}

	canaries, err := db.GetCanariesOfAgent(ctx, agent.ID, since)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Err: fmt.Sprintf("error fetching canaries for agent(name=%s)", agent.Name),
		})
	}

	return c.JSON(http.StatusOK, canaries)
}

// PullScrapeConfigs returns all scrape configs for the agent.
func PullScrapeConfigs(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	agent := ctx.Agent()

	var err error
	var since time.Time
	if sinceRaw := c.QueryParam("since"); sinceRaw != "" {
		since, err = time.Parse(time.RFC3339Nano, sinceRaw)
		if err != nil {
			return c.JSON(
				http.StatusBadRequest,
				dutyAPI.HTTPError{Err: fmt.Sprintf("'since' param needs to be a valid RFC3339Nano timestamp: %v", err)},
			)
		}

		ctx.GetSpan().SetAttributes(attribute.String("upstream.pull.configs.since", sinceRaw))
	}

	scrapeConfigs, err := db.GetScrapeConfigsOfAgent(ctx, agent.ID, since)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Err: fmt.Sprintf("error fetching scrape configs for agent(name=%s)", agent.Name),
		})
	}

	return c.JSON(http.StatusOK, scrapeConfigs)
}

func artifactsPushHandler(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	artifactID := c.Param("id")

	if err := artifacts.UploadArtifact(ctx, artifactID, c.Request().Body); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "ok"})
}
