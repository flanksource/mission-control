package push

import (
	"encoding/json"
	"io"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	dutydb "github.com/flanksource/duty/db"
	"github.com/flanksource/duty/models"
	dutytopology "github.com/flanksource/duty/topology"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func PushTopology(c echo.Context) error {
	if c.Request().Body == nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "request body is empty"))
	}
	defer c.Request().Body.Close()

	ctx := c.Request().Context().(context.Context)

	var data models.Component
	reqBody, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINTERNAL, "error reading request body: %v", err))
	}
	if err := json.Unmarshal(reqBody, &data); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "error unmarshaling json: %v", err))
	}

	agentID := uuid.Nil
	agentName := c.QueryParam("agentName")
	if agentName != "" {
		agent, err := db.GetAgent(ctx, agentName)
		if err != nil {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "agent [%s] not found: %v", agentName, err))
		}
		agentID = agent.ID
	}

	topologyObj := models.Topology{
		ID:        *data.TopologyID,
		AgentID:   agentID,
		Name:      data.Name,
		Namespace: data.Namespace,
		Labels:    data.Labels,
		Source:    models.SourcePush,
	}

	if err = topologyObj.Save(ctx.DB()); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINTERNAL, "error persisting topology: %v", dutydb.ErrorDetails(err)))
	}

	data.AgentID = agentID
	for _, c := range data.Components.Walk() {
		c.AgentID = agentID
		c.TopologyID = &topologyObj.ID
		// ConfigID will not be present in host and will cause FK Error
		c.ConfigID = nil
	}

	returnedIDs, err := dutytopology.SaveComponent(ctx, &data)
	if err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINTERNAL, "error saving components: %v", dutydb.ErrorDetails(err)))
	}

	var idsToDelete []string
	if err := ctx.DB().Model(&models.Component{}).Select("id").Where("topology_id = ?", data.TopologyID).Where("id NOT IN ?", returnedIDs).Find(&idsToDelete).Error; err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINTERNAL, "error querying old components: %v", dutydb.ErrorDetails(err)))
	}

	if len(idsToDelete) > 0 {
		if err := models.DeleteComponentsWithIDs(ctx.DB(), idsToDelete); err != nil {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINTERNAL, "error deleting old components: %v", dutydb.ErrorDetails(err)))
		}
	}

	return dutyAPI.WriteSuccess(c, data)
}
