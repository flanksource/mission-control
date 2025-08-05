package views

import (
	"net/http"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/utils"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /views routes")

	g := e.Group("/view", rbac.Authorization(policy.ObjectCatalog, policy.ActionRead))
	g.GET("/:namespace/:name", GetView)
	g.GET("/list", HandleViewList)
}

func GetView(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	namespace := c.Param("namespace")
	name := c.Param("name")

	cacheControl := c.Request().Header.Get("Cache-Control")
	headerMaxAge, headerRefreshTimeout, err := utils.ParseCacheControlHeader(cacheControl)
	if err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid cache control header: %s", err.Error()))
	}

	var opts []ViewOption
	if headerMaxAge > 0 {
		opts = append(opts, WithMaxAge(headerMaxAge))
	}
	if headerRefreshTimeout > 0 {
		opts = append(opts, WithRefreshTimeout(headerRefreshTimeout))
	}

	response, err := ReadOrPopulateViewTable(ctx, namespace, name, opts...)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}

// Takes config id as a query param and returns all the available views
// that can be placed on the given config.
func HandleViewList(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var configID = c.QueryParam("config_id")
	if configID == "" {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "config_id is required"))
	}

	views, err := listViewsForConfig(ctx, configID)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, views)
}

func listViewsForConfig(ctx context.Context, id string) ([]api.ViewListItem, error) {
	var config models.ConfigItem
	if err := ctx.DB().Where("id = ?", id).Find(&config).Error; err != nil {
		return nil, ctx.Oops("db").Wrap(err)
	} else if config.ID == uuid.Nil {
		return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("config(id=%s) not found", id)
	}

	list, err := db.FindViewsForConfig(ctx, config)
	return list, ctx.Oops().Wrap(err)
}
