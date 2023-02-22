package upstream

import (
	"encoding/json"
	"net/http"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

var dummyTemplateID *uuid.UUID

func PushUpstream(c echo.Context) error {
	var req api.PushData
	err := json.NewDecoder(c.Request().Body).Decode(&req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPErrorMessage{Error: err.Error(), Message: "invalid json request"})
	}

	if dummyTemplateID == nil {
		dummyTemplateID, err = db.GetDummyTemplateID(c.Request().Context())
		if err != nil {
			return c.JSON(http.StatusBadRequest, api.HTTPErrorMessage{Error: err.Error(), Message: "failed to get dummy template"})
		}
	}
	req.ReplaceTemplateID(dummyTemplateID)

	if err := db.InsertUpstreamMsg(c.Request().Context(), &req); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPErrorMessage{Error: err.Error(), Message: "something went wrong"}) // TODO: better error message
	}

	return nil
}
