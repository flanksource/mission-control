package upstream

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"
)

var templateIDCache = cache.New(3*24*time.Hour, 12*time.Hour)

func PushUpstream(c echo.Context) error {
	var req api.PushData
	err := json.NewDecoder(c.Request().Body).Decode(&req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error(), Message: "invalid json request"})
	}

	dummyTemplateID, ok := templateIDCache.Get(req.ClusterName)
	if !ok {
		dummyTemplate, err := db.GetOrCreateDummyTemplateID(c.Request().Context(), req.ClusterName)
		if err != nil {
			return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error(), Message: "failed to get dummy template"})
		}

		dummyTemplateID = &dummyTemplate.ID
		templateIDCache.Set(req.ClusterName, dummyTemplateID, cache.DefaultExpiration)
	}
	req.ReplaceTemplateID(dummyTemplateID.(*uuid.UUID))

	if err := db.InsertUpstreamMsg(c.Request().Context(), &req); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to upsert upstream message"})
	}

	return nil
}
