package application

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /application routes")

	g := e.Group(fmt.Sprintf("/%s", "application"), rbac.Authorization(policy.ObjectCatalog, policy.ActionRead))
	g.GET("/:namespace/:name", ApplicationSpec)
	g.GET("/:namespace/:name/export", ExportApplication)
}

func ApplicationSpec(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	namespace := c.Param("namespace")
	name := c.Param("name")

	application, err := db.FindApplication(ctx, namespace, name)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Errorf("failed to find application %s/%s: %w", namespace, name, err))
	} else if application == nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "application %s/%s not found", namespace, name))
	}

	app, err := v1.ApplicationFromModel(*application)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Errorf("failed to convert application: %w", err))
	}

	generated, err := buildApplication(ctx, app)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Errorf("failed to build application: %w", err))
	}

	specJSON, err := json.Marshal(generated)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Errorf("failed to marshal application: %w", err))
	}

	return c.JSONBlob(http.StatusOK, specJSON)
}

// ExportApplication renders the application in the requested format.
// Query params: format=(json|html|pdf|facet-html|facet-pdf), disposition=(inline|attachment)
func ExportApplication(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	namespace := c.Param("namespace")
	name := c.Param("name")
	format := c.QueryParam("format")
	if format == "" {
		format = "json"
	}
	disposition := c.QueryParam("disposition")

	data, err := Export(ctx, namespace, name, format)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	contentType, ext := formatContentType(format)
	if disposition == "attachment" {
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.%s"`, name, namespace, ext))
	}

	return c.Blob(http.StatusOK, contentType, data)
}

func formatContentType(format string) (contentType, ext string) {
	switch format {
	case "pdf", "facet-pdf":
		return "application/pdf", "pdf"
	case "html", "facet-html":
		return "text/html", "html"
	default:
		return "application/json", "json"
	}
}
