package artifacts

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/api"
	pkgConnection "github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/incident-commander/db"
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
	g.GET("/download/:id", DownloadArtifact)

}

func ListArtifacts(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	_id := c.Param("id")
	id, err := uuid.Parse(_id)
	if err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid id(%s). must be a uuid. %v", _id, err))
	}

	_checkTime := c.Param("check_time")
	checkTime, err := time.Parse(time.RFC3339, _checkTime)
	if err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid check_time(%s). must be in RFC3339", _checkTime))
	}

	var artifacts []models.Artifact
	if strings.Contains(c.Path(), "/list/check/") {
		artifacts, err = query.ArtifactsByCheck(ctx, id, checkTime)
		if err != nil {
			return api.WriteError(c, err)
		}
	} else {
		artifacts, err = query.ArtifactsByPlaybookRun(ctx, id)
		if err != nil {
			return api.WriteError(c, err)
		}
	}

	return c.JSON(http.StatusOK, artifacts)
}

func DownloadArtifact(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	_id := c.Param("id")
	artifactID, err := uuid.Parse(_id)
	if err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid id(%s). must be a uuid. %v", _id, err))
	}

	artifact, err := db.FindArtifact(ctx, artifactID)
	if err != nil {
		return api.WriteError(c, err)
	} else if artifact == nil {
		return api.WriteError(c, api.Errorf(api.ENOTFOUND, "artifact(%s) was not found", artifactID))
	}

	conn, err := pkgConnection.Get(ctx, artifact.ConnectionID.String())
	if err != nil {
		return api.WriteError(c, err)
	} else if conn == nil {
		return api.WriteError(c, api.Errorf(api.ENOTFOUND, "artifact's connection was not found"))
	}

	// TODO: Pool connection to the underlying filesystem
	fs, err := artifacts.GetFSForConnection(ctx, *conn)
	if err != nil {
		return api.WriteError(c, err)
	}
	defer fs.Close()

	file, err := fs.Read(ctx, artifact.Path)
	if err != nil {
		return api.WriteError(c, err)
	}
	defer file.Close()

	return c.Stream(http.StatusOK, artifact.ContentType, file)
}
