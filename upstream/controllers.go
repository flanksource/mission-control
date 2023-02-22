package upstream

import (
	"encoding/json"
	"net/http"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/labstack/echo/v4"
)

func PushUpstream(c echo.Context) error {
	var req api.PushData
	err := json.NewDecoder(c.Request().Body).Decode(&req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPErrorMessage{
			Error:   err.Error(),
			Message: "invalid json request",
		})
	}

	if err := db.InsertUpstreamMsg(c.Request().Context(), &req); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPErrorMessage{
			Error:   err.Error(),
			Message: "something went wrong", // TODO: better error message
		})
	}

	return nil
}
