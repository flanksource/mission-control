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

var templateIDCache = cache.New(3*24*time.Hour, 12*time.Hour)

func PushUpstream(c echo.Context) error {
	var req api.PushData
	err := json.NewDecoder(c.Request().Body).Decode(&req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error(), Message: "invalid json request"})
	}

	if strings.TrimSpace(req.ClusterName) == "" {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: "cluster_name name is required", Message: "cluster name is required"})
	}

	headlessTplID, ok := templateIDCache.Get(req.ClusterName)
	if !ok {
		headlessTpl, err := db.GetOrCreateHeadlessTemplateID(c.Request().Context(), req.ClusterName)
		if err != nil {
			return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error(), Message: fmt.Sprintf("failed to get headless template for cluster: %s", req.ClusterName)})
		}

		headlessTplID = &headlessTpl.ID
		templateIDCache.Set(req.ClusterName, headlessTplID, cache.DefaultExpiration)
	}
	req.ReplaceTemplateID(headlessTplID.(*uuid.UUID))

	if err := db.InsertUpstreamMsg(c.Request().Context(), &req); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "failed to upsert upstream message"})
	}

	return nil
}
