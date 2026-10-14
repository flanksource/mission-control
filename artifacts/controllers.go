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
	"github.com/flanksource/duty/query"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/membership"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

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
		if err := authorizeProducer(ctx, models.Artifact{CheckID: &id}); err != nil {
			return nil, err
		}
		return query.ArtifactsByCheck(ctx, id, checkTime)
	case strings.Contains(c.Path(), "/list/config_change/"):
		if err := authorizeProducer(ctx, models.Artifact{ConfigChangeID: &id}); err != nil {
			return nil, err
		}
		return query.ArtifactsByConfigChange(ctx, id)
	default:
		if err := authorizeRun(ctx, id); err != nil {
			return nil, err
		}
		var artifacts []models.Artifact
		err := ctx.DB().Model(&models.Artifact{}).
			Joins("JOIN playbook_run_actions ON playbook_run_actions.id = artifacts.playbook_run_action_id").
			Where("playbook_run_actions.playbook_run_id = ?", id).Find(&artifacts).Error
		return artifacts, err
	}
}

// producerError hides whether a producer is missing or unreadable.
func producerError(err error) error {
	if err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
		return api.Errorf(api.ENOTFOUND, "artifact or producer not found")
	}
	return err
}

// authorizeProducer requires read on every producer an artifact names; unowned artifacts are never readable.
func authorizeProducer(ctx context.Context, artifact models.Artifact) error {
	var attrs []*models.ABACAttribute
	if artifact.CheckID != nil {
		var check models.Check
		if err := ctx.DB().Where("deleted_at IS NULL").First(&check, artifact.CheckID).Error; err != nil {
			return producerError(err)
		}
		attrs = append(attrs, &models.ABACAttribute{Check: check})
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
		attrs = append(attrs, &models.ABACAttribute{Config: config})
	}
	if err := authorizeReads(ctx, attrs...); err != nil {
		return err
	}
	if artifact.PlaybookRunActionID != nil {
		var action models.PlaybookRunAction
		if err := ctx.DB().First(&action, artifact.PlaybookRunActionID).Error; err != nil {
			return producerError(err)
		}
		if err := authorizeRun(ctx, action.PlaybookRunID); err != nil {
			return err
		}
	}
	if artifact.ScraperID != nil {
		// Scrapers have no ABAC attribute; their token-limited RLS rows decide scoped reads.
		if err := auth.WithRLS(ctx, func(ctx context.Context) error {
			var scraper models.ConfigScraper
			return ctx.DB().Where("deleted_at IS NULL").First(&scraper, artifact.ScraperID).Error
		}); err != nil {
			return producerError(err)
		}
	}
	if len(attrs) == 0 && artifact.PlaybookRunActionID == nil && artifact.ScraperID == nil {
		return producerError(nil)
	}
	return nil
}

// authorizeRun follows playbook_runs RLS: read the playbook and each config/check target, not the component.
func authorizeRun(ctx context.Context, id uuid.UUID) error {
	var run models.PlaybookRun
	if err := ctx.DB().First(&run, id).Error; err != nil {
		return producerError(err)
	}
	run.ComponentID = nil
	attr, err := run.GetABACAttributes(ctx.DB())
	if err != nil {
		return producerError(err)
	}
	if attr.Playbook.DeletedAt != nil || attr.Config.DeletedAt != nil || attr.Check.DeletedAt != nil {
		return producerError(nil)
	}
	attrs := []*models.ABACAttribute{{Playbook: attr.Playbook}}
	if run.ConfigID != nil {
		attrs = append(attrs, &models.ABACAttribute{Config: attr.Config})
	}
	if run.CheckID != nil {
		attrs = append(attrs, &models.ABACAttribute{Check: attr.Check})
	}
	return authorizeReads(ctx, attrs...)
}

func authorizeReads(ctx context.Context, attrs ...*models.ABACAttribute) error {
	checkCtx, err := membership.ForOperation(ctx, dutyRBAC.MembershipRefs(attrs...)...)
	if err != nil {
		return err
	}
	for _, attr := range attrs {
		if !rbac.HasPermission(checkCtx, ctx.Subject(), attr, policy.ActionRead) {
			return producerError(nil)
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

	if visible, err := auth.CanReadArtifact(ctx, artifactID); err != nil {
		return api.WriteError(c, err)
	} else if !visible {
		return api.WriteError(c, producerError(nil))
	}

	var artifact models.Artifact
	if err := ctx.DB().Where("deleted_at IS NULL").First(&artifact, artifactID).Error; err != nil {
		return api.WriteError(c, producerError(err))
	}
	if err := authorizeProducer(ctx, artifact); err != nil {
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
