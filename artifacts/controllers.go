package artifacts

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/incident-commander/auth"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /artifacts routes")

	g := e.Group(fmt.Sprintf("/%s", "artifacts"), rbac.Authorization(policy.ObjectArtifact, policy.ActionRead))
	g.GET("/list/check/:id/:check_time", ListArtifacts)
	g.GET("/list/playbook_run/:id", ListArtifacts)
	g.GET("/list/config_change/:id", ListArtifacts)
	g.GET("/download/:id", DownloadArtifact)

}

func ListArtifacts(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	_id := c.Param("id")
	id, err := uuid.Parse(_id)
	if err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid id(%s). must be a uuid. %v", _id, err))
	}

	var artifacts []models.Artifact
	err = auth.WithRLS(ctx, func(ctx context.Context) error {
		var err error
		artifacts, err = listArtifacts(ctx, c, id)
		return err
	})
	if err != nil {
		return api.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, artifacts)
}

// listArtifacts returns the artifacts of the check, config change or playbook run the request names.
func listArtifacts(ctx context.Context, c echo.Context, id uuid.UUID) ([]models.Artifact, error) {
	switch {
	case strings.Contains(c.Path(), "/list/check/"):
		_checkTime := c.Param("check_time")
		checkTime, err := time.Parse(time.RFC3339, _checkTime)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "invalid check_time(%s). must be in RFC3339", _checkTime)
		}
		return query.ArtifactsByCheck(ctx, id, checkTime)
	case strings.Contains(c.Path(), "/list/config_change/"):
		return query.ArtifactsByConfigChange(ctx, id)
	default:
		return query.ArtifactsByPlaybookRun(ctx, id)
	}
}

func DownloadArtifact(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	artifactID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid id: %v", err))
	}

	if visible, err := auth.CanReadArtifact(ctx, artifactID); err != nil {
		return api.WriteError(c, err)
	} else if !visible {
		return api.WriteError(c, api.Errorf(api.ENOTFOUND, "artifact %s not found", artifactID))
	}

	blobs, err := ctx.Blobs()
	if err != nil {
		return api.WriteError(c, err)
	}
	defer blobs.Close()

	data, err := blobs.Read(artifactID)
	if err != nil {
		return api.WriteError(c, err)
	}
	defer data.Content.Close()

	c.Response().Header().Set("Content-Type", data.ContentType)
	if data.ContentLength > 0 {
		c.Response().Header().Set("Content-Length", fmt.Sprintf("%d", data.ContentLength))
	}
	if filename := c.QueryParam("filename"); filename != "" {
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	}
	return c.Stream(http.StatusOK, data.ContentType, data.Content)
}
