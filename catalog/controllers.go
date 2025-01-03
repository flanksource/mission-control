package catalog

import (
	"net/http"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/policy"
	"github.com/labstack/echo/v4"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /catalog routes")

	apiGroup := e.Group("/catalog", rbac.Catalog(policy.ActionRead))
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
