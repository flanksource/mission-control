package catalog

import (
	"net/http"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/labstack/echo/v4"
)

func RegisterRoutes(e *echo.Echo) {
	apiGroup := e.Group("/catalog", rbac.Catalog(rbac.ActionRead))
	apiGroup.POST("/summary", SearchConfigSummary)

	apiGroup.POST("/changes", SearchCatalogChanges)
	// Deprecated. Use POST
	apiGroup.GET("/changes", SearchCatalogChanges)
	apiGroup.POST("/summary", SearchConfigSummary)
}

func SearchCatalogChanges(c echo.Context) error {
	var req query.CatalogChangesSearchRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request: %v", err))
	}

	ctx := c.Request().Context().(context.Context)

	response, err := query.FindCatalogChanges(ctx, req)
	if err != nil {
		return api.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}

func SearchConfigSummary(c echo.Context) error {
	var req query.ConfigSummaryRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request: %v", err))
	}

	ctx := c.Request().Context().(context.Context)
	response, err := query.ConfigSummary(ctx, req)
	if err != nil {
		return api.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}
