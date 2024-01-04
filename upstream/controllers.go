package upstream

import (
	"fmt"
	"net/http"
	"time"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/upstream"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"
	"go.opentelemetry.io/otel/attribute"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
)

var (
	agentIDCache = cache.New(3*24*time.Hour, 12*time.Hour)
)

func RegisterRoutes(e *echo.Echo) {
	upstreamGroup := e.Group("/upstream", rbac.Authorization(rbac.ObjectAgentPush, rbac.ActionWrite))
	upstreamGroup.POST("/push", upstream.PushHandler(agentIDCache))
	upstreamGroup.GET("/pull/:agent_name", upstream.PullHandler(api.TablesToReconcile))
	upstreamGroup.GET("/status/:agent_name", upstream.StatusHandler(api.TablesToReconcile))
	upstreamGroup.GET("/canary/pull/:agent_name", PullCanaries)
	upstreamGroup.GET("/scrapeconfig/pull/:agent_name", PullScrapeConfigs)
}

// PullCanaries returns all canaries for the  agent
func PullCanaries(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	agentName := c.Param("agent_name")
	agent, err := db.FindAgent(ctx, agentName)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Error: err.Error(), Message: "failed to get agent"})
	} else if agent == nil {
		return c.JSON(http.StatusNotFound, dutyAPI.HTTPError{Message: fmt.Sprintf("agent(name=%s) not found", agentName)})
	}

	var since time.Time
	if sinceRaw := c.QueryParam("since"); sinceRaw != "" {
		since, err = time.Parse(time.RFC3339, sinceRaw)
		if err != nil {
			return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Error: err.Error(), Message: "'since' param needs to be a valid RFC3339 timestamp"})
		}

		ctx.GetSpan().SetAttributes(attribute.String("upstream.pull.canaries.since", sinceRaw))
	}

	canaries, err := db.GetCanariesOfAgent(ctx, agent.ID, since)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Error:   err.Error(),
			Message: fmt.Sprintf("Error fetching canaries for agent(name=%s)", agentName),
		})
	}

	return c.JSON(http.StatusOK, canaries)
}

// PullScrapeConfigs returns all scrape configs for the agent.
func PullScrapeConfigs(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	agentName := c.Param("agent_name")

	agent, err := db.FindAgent(ctx, agentName)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Error: err.Error(), Message: "failed to get agent"})
	} else if agent == nil {
		return c.JSON(http.StatusNotFound, dutyAPI.HTTPError{Message: fmt.Sprintf("agent(name=%s) not found", agentName)})
	}

	var since time.Time
	if sinceRaw := c.QueryParam("since"); sinceRaw != "" {
		since, err = time.Parse(time.RFC3339Nano, sinceRaw)
		if err != nil {
			return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{Error: err.Error(), Message: "'since' param needs to be a valid RFC3339Nano timestamp"})
		}

		ctx.GetSpan().SetAttributes(attribute.String("upstream.pull.configs.since", sinceRaw))
	}

	scrapeConfigs, err := db.GetScrapeConfigsOfAgent(ctx, agent.ID, since)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Error:   err.Error(),
			Message: fmt.Sprintf("error fetching scrape configs for agent(name=%s)", agentName),
		})
	}

	return c.JSON(http.StatusOK, scrapeConfigs)
}
