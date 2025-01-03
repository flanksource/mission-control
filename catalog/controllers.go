package catalog

import (
	"encoding/json"
	"net/http"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/auth"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/policy"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel/trace"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /catalog routes")

	apiGroup := e.Group("/catalog", rbac.Catalog(policy.ActionRead))
	apiGroup.POST("/summary", SearchConfigSummary)

	apiGroup.POST("/changes", SearchCatalogChanges, rlsMiddleware)
	// Deprecated. Use POST
	apiGroup.GET("/changes", SearchCatalogChanges, rlsMiddleware)
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

func rlsMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context().(context.Context)

		rlsPayload, err := auth.GetRLSPayload(ctx)
		if err != nil {
			return err
		}

		if rlsPayload.Disable {
			return next(c)
		}

		rlsJSON, err := json.Marshal(rlsPayload)
		if err != nil {
			return err
		}

		err = ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
			if err := txCtx.DB().Exec("SET LOCAL ROLE postgrest_api").Error; err != nil {
				return err
			}

			if err := txCtx.DB().Exec(`SET LOCAL request.jwt.claims = ?`, string(rlsJSON)).Error; err != nil {
				return err
			}

			// set the context with the tx
			c.SetRequest(c.Request().WithContext(txCtx))

			return next(c)
		})

		return err
	}
}
