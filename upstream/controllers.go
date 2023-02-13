package upstream

import (
	"encoding/json"
	"net/http"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/labstack/echo/v4"
)

func UpstreamPushesCtrl(c echo.Context) error {
	var req api.ConfigChanges
	err := json.NewDecoder(c.Request().Body).Decode(&req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPErrorMessage{
			Error:   err.Error(),
			Message: "invalid json request",
		})
	}

	logger.Infof("%v", req)
	return nil
}
