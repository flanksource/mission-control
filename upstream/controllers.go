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

	req.ClusterName = strings.TrimSpace(req.ClusterName)
	if req.ClusterName == "" {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: "cluster_name name is required", Message: "cluster name is required"})
	}

	agentID, ok := agentIDCache.Get(req.ClusterName)
	if !ok {
		agent, err := db.GetOrCreateAgent(req.ClusterName)
		if err != nil {
			return c.JSON(http.StatusBadRequest, api.HTTPError{
				Error:   err.Error(),
				Message: "Error while creating/fetching agent",
			})
		}
		agentID = &agent.ID
		agentIDCache.Set(req.ClusterName, agentID, cache.DefaultExpiration)
	}

	headlessTopologyID, ok := topologyIDCache.Get(req.ClusterName)
	if !ok {
		headlessTopology, err := db.GetOrCreateHeadlessTopology(c.Request().Context(), req.ClusterName)
		if err != nil {
			return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error(), Message: fmt.Sprintf("failed to get headless topology for cluster: %s", req.ClusterName)})
		}

		headlessTopologyID = &headlessTopology.ID
		topologyIDCache.Set(req.ClusterName, headlessTopologyID, cache.DefaultExpiration)
	}
	req.ReplaceTopologyID(headlessTopologyID.(*uuid.UUID))
	req.PopulateAgentID(agentID.(*uuid.UUID))

	if err := db.InsertUpstreamMsg(c.Request().Context(), &req); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to upsert upstream message"})
	}

	return nil
}
