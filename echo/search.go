package echo

import (
	"encoding/json"
	"net/http"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/rbac/policy"
	echov4 "github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/rbac"
)

func SearchResources(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)

	var request query.SearchResourcesRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&request); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "%s", err.Error()))
	}

	// Connections aren't filtered by row, so they're searched only with read on all of them
	if len(request.Connections) > 0 && !rbac.CheckContext(ctx, policy.ObjectConnection, policy.ActionRead) {
		request.Connections = nil
	}

	response, err := query.SearchResources(ctx, request)
	if err != nil {
		return api.WriteError(c, err)
	}

	if wantsClicky(c.Request().Header.Get("Accept")) {
		body, err := renderSearchClicky(response)
		if err != nil {
			return api.WriteError(c, err)
		}
		return c.Blob(http.StatusOK, "application/json+clicky", []byte(body))
	}

	return c.JSON(http.StatusOK, response)
}
