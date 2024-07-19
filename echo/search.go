package echo

import (
	"encoding/json"
	"net/http"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	echov4 "github.com/labstack/echo/v4"
)

func SearchResources(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)

	var request query.SearchResourcesRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&request); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, err.Error()))
	}

	response, err := query.SearchResources(ctx, request)
	if err != nil {
		return api.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}
