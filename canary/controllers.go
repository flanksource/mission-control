package canary

import (
	"fmt"
	"net/http"
	"time"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/labstack/echo/v4"
)

// Pull returns all canaries for the requested agent
func Pull(c echo.Context) error {
	ctx := c.(*api.Context)

	agentName := c.Param("agent_name")

	agent, err := db.FindAgent(ctx, agentName)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to get agent"})
	} else if agent == nil {
		return c.JSON(http.StatusNotFound, api.HTTPError{Message: fmt.Sprintf("agent(name=%s) not found", agentName)})
	}

	var since time.Time
	if sinceRaw := c.QueryParam("since"); sinceRaw != "" {
		since, err = time.Parse(time.RFC3339, sinceRaw)
		if err != nil {
			return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error(), Message: "'since' param needs to be a valid RFC3339 timestamp"})
		}
	}

	canaries, err := db.GetCanariesOfAgent(c.Request().Context(), agent.ID, since)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: fmt.Sprintf("Error fetching canaries for agent(name=%s)", agentName),
		})
	}

	return c.JSON(http.StatusOK, canaries)
}
