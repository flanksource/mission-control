package agent

import (
	"encoding/json"
	"net/http"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/rbac"
)

// RegisterRoutes registers the agent routes.
func RegisterRoutes(e *echo.Echo) {
	e.POST("/agent/generate", GenerateAgent, rbac.Authorization(policy.ObjectAgent, policy.ActionCreate))
	e.POST("/agent/token", GenerateToken, rbac.Authorization(policy.ObjectAgent, policy.ActionUpdate))
}

// GenerateAgent creates a new person and a new agent and associates them.
func GenerateAgent(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var body api.GenerateAgentRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&body); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}

	agent, err := generateAgent(ctx, body)
	if err != nil {
		logger.Errorf("failed to generate a new agent: %v", err)
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	return c.JSON(http.StatusCreated, agent)
}

// GenerateToken creates a new token for an existing agent
func GenerateToken(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var body api.GenerateTokenRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&body); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}

	token, err := generateToken(ctx, body)
	if err != nil {
		logger.Errorf("failed to generate a new token: %v", err)
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	return c.JSON(http.StatusCreated, token)
}
