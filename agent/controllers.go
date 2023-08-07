package agent

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/flanksource/commons/logger"
	crand "github.com/flanksource/commons/rand"
	"github.com/flanksource/incident-commander/api"
	"github.com/labstack/echo/v4"
)

// GenerateAgent creates a new person and a new agent and associates them.
func GenerateAgent(c echo.Context) error {
	ctx := c.(*api.Context)

	var body api.GenerateAgentRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&body); err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error()})
	}

	agent, err := generateAgent(ctx, body)
	if err != nil {
		logger.Errorf("failed to generate a new agent: %v", err)
		c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "error generating agent"})
	}

	return c.JSON(http.StatusCreated, agent)
}

func genUsernamePassword() (username, password string, err error) {
	username, err = crand.GenerateRandHex(8)
	if err != nil {
		return "", "", err
	}

	password, err = crand.GenerateRandHex(32)
	if err != nil {
		return "", "", err
	}

	return fmt.Sprintf("agent-%s", username), password, nil
}
