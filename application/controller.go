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

const (
	defaultFormat      = "html"
	defaultDisposition = "inline"
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

	response, err := applicationSpec(ctx, namespace, name)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSONBlob(http.StatusOK, response)
}

// ExportApplication renders the application as HTML or PDF and writes it to the response.
// Query params: format=(html|pdf), disposition=(inline|attachment)
func ExportApplication(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	namespace := c.Param("namespace")
	name := c.Param("name")

	format := c.QueryParam("format")
	if format == "" {
		format = defaultFormat
	}
	disposition := c.QueryParam("disposition")
	if disposition == "" {
		disposition = defaultDisposition
	}

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

	switch format {
	case "pdf":
		data, err := RenderPDF(generated)
		if err != nil {
			return dutyAPI.WriteError(c, ctx.Oops().Errorf("failed to render PDF: %w", err))
		}
		filename := fmt.Sprintf("%s-%s.pdf", name, namespace)
		if disposition == "attachment" {
			c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		} else {
			c.Response().Header().Set("Content-Disposition", "inline")
		}
		return c.Blob(http.StatusOK, "application/pdf", data)
	default:
		html, err := RenderHTML(generated)
		if err != nil {
			return dutyAPI.WriteError(c, ctx.Oops().Errorf("failed to render HTML: %w", err))
		}
		filename := fmt.Sprintf("%s-%s.html", name, namespace)
		if disposition == "attachment" {
			c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		} else {
			c.Response().Header().Set("Content-Disposition", "inline")
		}
		return c.HTMLBlob(http.StatusOK, []byte(html))
	}
}

func applicationSpec(ctx context.Context, namespace, name string) ([]byte, error) {
	application, err := db.FindApplication(ctx, namespace, name)
	if err != nil {
		return nil, ctx.Oops().Errorf("failed to find application %s/%s: %w", namespace, name, err)
	} else if application == nil {
		return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("application %s/%s not found", namespace, name)
	}

	app, err := v1.ApplicationFromModel(*application)
	if err != nil {
		return nil, ctx.Oops().Errorf("failed to convert application to kubernetes application: %w", err)
	}

	generated, err := buildApplication(ctx, app)
	if err != nil {
		return nil, ctx.Oops().Errorf("failed to generate application: %w", err)
	}

	specJSON, err := json.Marshal(generated)
	if err != nil {
		return nil, ctx.Oops().Errorf("failed to marshal generated application: %w", err)
	}

	return specJSON, nil
}
