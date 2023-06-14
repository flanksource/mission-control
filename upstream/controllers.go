package upstream

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"
)

var (
	topologyIDCache = cache.New(3*24*time.Hour, 12*time.Hour)
	agentIDCache    = cache.New(3*24*time.Hour, 12*time.Hour)
)

func PushUpstream(c echo.Context) error {
	var req api.PushData
	err := json.NewDecoder(c.Request().Body).Decode(&req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error(), Message: "invalid json request"})
	}

	req.AgentName = strings.TrimSpace(req.AgentName)
	if req.AgentName == "" {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: "cluster_name name is required", Message: "cluster name is required"})
	}

	agentID, ok := agentIDCache.Get(req.AgentName)
	if !ok {
		agent, err := db.GetOrCreateAgent(req.AgentName)
		if err != nil {
			return c.JSON(http.StatusBadRequest, api.HTTPError{
				Error:   err.Error(),
				Message: "Error while creating/fetching agent",
			})
		}
		agentID = agent.ID
		agentIDCache.Set(req.AgentName, agentID, cache.DefaultExpiration)
	}

	headlessTopologyID, ok := topologyIDCache.Get(req.AgentName)
	if !ok {
		headlessTopology, err := db.GetOrCreateHeadlessTopology(c.Request().Context(), req.AgentName)
		if err != nil {
			return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error(), Message: fmt.Sprintf("failed to get headless topology for cluster: %s", req.AgentName)})
		}

		headlessTopologyID = &headlessTopology.ID
		topologyIDCache.Set(req.AgentName, headlessTopologyID, cache.DefaultExpiration)
	}
	req.ReplaceTopologyID(headlessTopologyID.(*uuid.UUID))
	req.PopulateAgentID(agentID.(uuid.UUID))

	if err := db.InsertUpstreamMsg(c.Request().Context(), &req); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to upsert upstream message"})
	}

	return nil
}

// Reconcile returns all the ids of items it has
func Reconcile(c echo.Context) error {
	ctx := c.Request().Context()

	agentName := c.Param("agent_name")
	agent, err := db.FindAgent(ctx, agentName)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to get agent"})
	} else if agent == nil {
		return c.JSON(http.StatusNotFound, api.HTTPError{Message: fmt.Sprintf("agent(name=%s) not found", agentName)})
	}

	resp, err := db.GetAllResourceIDsOfAgent(ctx, agent.ID.String())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to get resource ids"})
	}

	return c.JSON(http.StatusFound, resp)
}
