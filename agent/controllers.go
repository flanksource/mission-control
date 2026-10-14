package agent

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
)

// RegisterRoutes registers the agent routes.
func RegisterRoutes(e *echo.Echo) {
	e.POST("/agent/generate", GenerateAgent, rbac.Authorization(policy.ObjectAgent, policy.ActionCreate))
	e.POST("/agent/token", GenerateToken, rbac.Authorization(policy.ObjectAgent, policy.ActionUpdate))
	e.GET("/agent/:id/tokens", ListTokens, rbac.Authorization(policy.ObjectAgent, policy.ActionRead))
	e.DELETE("/agent/:id/tokens/:tokenID", DeleteToken, rbac.Authorization(policy.ObjectAgent, policy.ActionUpdate))
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

	if rbac.MadeWithAccessToken(ctx) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "access tokens can't be created with an access token"))
	}

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

// ListTokens lists an agent's access tokens, without their values.
func ListTokens(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	agent, err := findAgent(ctx, c.Param("id"))
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	tokens := []models.AccessToken{}
	if agent.PersonID != nil {
		if tokens, err = db.ListPersonAccessTokens(ctx, *agent.PersonID); err != nil {
			return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "failed to list the access tokens of agent %s", agent.ID))
		}
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "success", Payload: tokens})
}

// DeleteToken deletes one of an agent's access tokens. It stops working at once.
func DeleteToken(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	if rbac.MadeWithAccessToken(ctx) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "access tokens can't be deleted with an access token"))
	}

	agent, err := findAgent(ctx, c.Param("id"))
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	tokenID := c.Param("tokenID")
	if uuid.Validate(tokenID) != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid token id %q", tokenID))
	}

	// A token of another agent, or of a person, is reported as missing
	token, err := db.GetAccessToken(ctx, tokenID)
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && (agent.PersonID == nil || token.PersonID != *agent.PersonID)) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "agent %s has no access token %s", agent.ID, tokenID))
	} else if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "failed to get access token %s", tokenID))
	}

	if err := auth.DeleteAccessToken(ctx, tokenID); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "failed to delete access token %s", tokenID))
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "success"})
}

// findAgent returns the agent with the id, or an ENOTFOUND error.
func findAgent(ctx context.Context, id string) (*models.Agent, error) {
	if uuid.Validate(id) != nil {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid agent id %q", id)
	}

	var agents []models.Agent
	if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", id).Limit(1).Find(&agents).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to get agent %s", id)
	} else if len(agents) == 0 {
		return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "agent %s not found", id)
	}
	return &agents[0], nil
}
