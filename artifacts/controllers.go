package artifacts

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/auth"
	echoSrv "github.com/flanksource/incident-commander/echo"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /artifacts routes")

	g := e.Group("/artifacts")
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
		if err != nil {
			return err
		}
		active := artifacts[:0]
		for _, artifact := range artifacts {
			if err := findProducer(ctx, artifact); err != nil {
				if api.ErrorCode(err) == api.ENOTFOUND {
					continue
				}
				return err
			}
			active = append(active, artifact)
		}
		artifacts = active
		return nil
	})
	if err != nil {
		return api.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, artifacts)
}

// listArtifacts returns the artifacts of the check, config change or playbook run the request names.
func listArtifacts(ctx context.Context, c echo.Context, id uuid.UUID) ([]models.Artifact, error) {
	q := ctx.DB().Model(&models.Artifact{}).Where("artifacts.deleted_at IS NULL")
	switch {
	case strings.Contains(c.Path(), "/list/check/"):
		_checkTime := c.Param("check_time")
		checkTime, err := time.Parse(time.RFC3339, _checkTime)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "invalid check_time(%s). must be in RFC3339", _checkTime)
		}
		if err := findProducer(ctx, models.Artifact{CheckID: &id}); err != nil {
			return nil, err
		}
		q = q.Where("artifacts.check_id = ? AND artifacts.check_time = ?", id, checkTime)
	case strings.Contains(c.Path(), "/list/config_change/"):
		if err := findProducer(ctx, models.Artifact{ConfigChangeID: &id}); err != nil {
			return nil, err
		}
		q = q.Where("artifacts.config_change_id = ?", id)
	default:
		if err := findRun(ctx, id); err != nil {
			return nil, err
		}
		q = q.
			Joins("JOIN playbook_run_actions ON playbook_run_actions.id = artifacts.playbook_run_action_id").
			Where("playbook_run_actions.playbook_run_id = ?", id)
	}
	var artifacts []models.Artifact
	err := q.Find(&artifacts).Error
	return artifacts, err
}

// producerError hides whether a producer is missing or unreadable.
func producerError(err error) error {
	if err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
		return api.Errorf(api.ENOTFOUND, "artifact or producer not found")
	}
	return err
}

// findProducer rejects unowned artifacts and missing/deleted producers. The caller must use WithRLS;
// PostgreSQL decides readability, including check access inherited from a canary.
func findProducer(ctx context.Context, artifact models.Artifact) error {
	if artifact.CheckID != nil {
		var check models.Check
		if err := ctx.DB().Where("deleted_at IS NULL").First(&check, artifact.CheckID).Error; err != nil {
			return producerError(err)
		}
	}
	if artifact.ConfigChangeID != nil {
		var change models.ConfigChange
		if err := ctx.DB().First(&change, artifact.ConfigChangeID).Error; err != nil {
			return producerError(err)
		}
		var config models.ConfigItem
		if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", change.ConfigID).First(&config).Error; err != nil {
			return producerError(err)
		}
	}
	if artifact.PlaybookRunActionID != nil {
		var action models.PlaybookRunAction
		if err := ctx.DB().First(&action, artifact.PlaybookRunActionID).Error; err != nil {
			return producerError(err)
		}
		if err := findRun(ctx, action.PlaybookRunID); err != nil {
			return err
		}
	}
	if artifact.ScraperID != nil {
		var scraper models.ConfigScraper
		if err := ctx.DB().Where("deleted_at IS NULL").First(&scraper, artifact.ScraperID).Error; err != nil {
			return producerError(err)
		}
	}
	if artifact.CheckID == nil && artifact.ConfigChangeID == nil && artifact.PlaybookRunActionID == nil && artifact.ScraperID == nil {
		return producerError(nil)
	}
	return nil
}

// findRun checks live producers under RLS; component targets do not restrict run visibility.
func findRun(ctx context.Context, id uuid.UUID) error {
	var run models.PlaybookRun
	if err := ctx.DB().First(&run, id).Error; err != nil {
		return producerError(err)
	}
	var playbook models.Playbook
	if err := ctx.DB().Where("deleted_at IS NULL").First(&playbook, run.PlaybookID).Error; err != nil {
		return producerError(err)
	}
	if run.ConfigID != nil {
		var config models.ConfigItem
		if err := ctx.DB().Where("deleted_at IS NULL").First(&config, run.ConfigID).Error; err != nil {
			return producerError(err)
		}
	}
	if run.CheckID != nil {
		var check models.Check
		if err := ctx.DB().Where("deleted_at IS NULL").First(&check, run.CheckID).Error; err != nil {
			return producerError(err)
		}
	}
	return nil
}

func DownloadArtifact(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	artifactID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid id: %v", err))
	}

	var artifact models.Artifact
	if err := auth.WithRLS(ctx, func(ctx context.Context) error {
		if err := ctx.DB().Where("deleted_at IS NULL").First(&artifact, artifactID).Error; err != nil {
			return producerError(err)
		}
		return findProducer(ctx, artifact)
	}); err != nil {
		return api.WriteError(c, err)
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
