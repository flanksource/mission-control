package upstream

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"
)

var (
	agentIDCache = cache.New(3*24*time.Hour, 12*time.Hour)
)

// PushUpstream saves the push data from agents.
func PushUpstream(c echo.Context) error {
	ctx := c.(api.Context)

	var req upstream.PushData
	err := json.NewDecoder(c.Request().Body).Decode(&req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error(), Message: "invalid json request"})
	}

	req.AgentName = strings.TrimSpace(req.AgentName)
	if req.AgentName == "" {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: "agent name is required", Message: "agent name is required"})
	}

	agentID, ok := agentIDCache.Get(req.AgentName)
	if !ok {
		agent, err := db.GetOrCreateAgent(ctx, req.AgentName)
		if err != nil {
			return c.JSON(http.StatusBadRequest, api.HTTPError{
				Error:   err.Error(),
				Message: "Error while creating/fetching agent",
			})
		}
		agentID = agent.ID
		agentIDCache.Set(req.AgentName, agentID, cache.DefaultExpiration)
	}

	req.PopulateAgentID(agentID.(uuid.UUID))

	logger.Tracef("Inserting push data %s", req.String())
	if err := db.InsertUpstreamMsg(ctx, &req); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to upsert upstream message"})
	}

	return nil
}

// Pull returns all the ids of items it has received from the requested agent.
func Pull(c echo.Context) error {
	ctx := c.(api.Context)

	var req upstream.PaginateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error()})
	}

	if !collections.Contains(api.TablesToReconcile, req.Table) {
		return c.JSON(http.StatusForbidden, api.HTTPError{Error: fmt.Sprintf("table=%s is not allowed", req.Table)})
	}

	agentName := c.Param("agent_name")
	agent, err := db.FindAgent(ctx, agentName)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to get agent"})
	} else if agent == nil {
		return c.JSON(http.StatusNotFound, api.HTTPError{Message: fmt.Sprintf("agent(name=%s) not found", agentName)})
	}

	resp, err := db.GetAllResourceIDsOfAgent(ctx, req, agent.ID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to get resource ids"})
	}

	return c.JSON(http.StatusFound, resp)
}

// Status returns the summary of all ids the upstream has received.
func Status(c echo.Context) error {
	ctx := c.(api.Context)

	var req upstream.PaginateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error()})
	}

	if !collections.Contains(api.TablesToReconcile, req.Table) {
		return c.JSON(http.StatusForbidden, api.HTTPError{Error: fmt.Sprintf("table=%s is not allowed", req.Table)})
	}

	var agentName = c.Param("agent_name")
	agent, err := db.FindAgent(ctx, agentName)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to get agent"})
	} else if agent == nil {
		return c.JSON(http.StatusNotFound, api.HTTPError{Message: fmt.Sprintf("agent(name=%s) not found", agentName)})
	}

	response, err := upstream.GetPrimaryKeysHash(ctx, req, agent.ID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to push status response"})
	}

	return c.JSON(http.StatusOK, response)
}
