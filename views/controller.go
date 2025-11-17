package views

import (
	"net/http"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/gomplate/v3"
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

	g := e.Group("/view", rbac.Authorization(policy.ObjectViews, policy.ActionRead))
	g.GET("/list", HandleViewList)
	g.GET("/display-plugin-variables/:viewID", GetDisplayPluginsVariables)

	// Deprecated: Use POST request
	g.GET("/:namespace/:name", GetViewByNamespaceName)
	g.GET("/:id", GetViewByID)

	g.POST("/:namespace/:name", GetViewByNamespaceName)
	g.POST("/:id", GetViewByID)
}

func GetDisplayPluginsVariables(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	viewID := c.Param("viewID")
	configID := c.QueryParam("config_id")
	if configID == "" {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "config_id is required"))
	}

	plugins, err := db.GetDisplayPlugins(ctx, viewID)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	config, err := query.GetCachedConfig(ctx, configID)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	} else if config == nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "config(id=%s) not found", configID))
	}

	for _, p := range plugins {
		if p.ConfigTab.IsEmpty() {
			continue
		}

		env := map[string]any{
			"config": config.AsMap(),
		}

		output := make(map[string]string)
		for k, v := range p.Variables {
			output[k], err = ctx.RunTemplate(gomplate.Template{Template: v}, env)
			if err != nil {
				return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
			}
		}

		// We return on first match.
		// We expect only one plugin to have config tab matching the config
		return c.JSON(http.StatusOK, output)
	}

	return c.JSON(http.StatusOK, map[string]string{})
}

func GetViewByID(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	id := c.Param("id")

	var view models.View
	if err := ctx.DB().Select("id, namespace, name").Where("id = ?", id).Where("deleted_at IS NULL").Find(&view).Error; err != nil {
		return dutyAPI.WriteError(c, err)
	} else if view.ID == uuid.Nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "view(id=%s) not found", id))
	}

	// Check ABAC permissions for this specific view
	attr := &models.ABACAttribute{
		View: view,
	}
	if !dutyRBAC.HasPermission(ctx, ctx.Subject(), attr, policy.ActionRead) {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("access denied to view %s/%s", view.Namespace, view.Name))
	}

	return getViewByNamespaceName(ctx, c, view.Namespace, view.Name)
}

func GetViewByNamespaceName(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	namespace := c.Param("namespace")
	name := c.Param("name")

	// Fetch the view to check ABAC permissions
	var view models.View
	if err := ctx.DB().Select("id, namespace, name").Where("namespace = ? AND name = ?", namespace, name).Where("deleted_at IS NULL").Find(&view).Error; err != nil {
		return dutyAPI.WriteError(c, err)
	} else if view.ID == uuid.Nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "view(namespace=%s, name=%s) not found", namespace, name))
	}

	// Check ABAC permissions for this specific view
	attr := &models.ABACAttribute{
		View: view,
	}
	if !dutyRBAC.HasPermission(ctx, ctx.Subject(), attr, policy.ActionRead) {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("access denied to view %s/%s", view.Namespace, view.Name))
	}

	return getViewByNamespaceName(ctx, c, namespace, name)
}

type viewRequestPostBody struct {
	Variables map[string]string `json:"variables"`
}

func getViewByNamespaceName(ctx context.Context, c echo.Context, namespace, name string) error {
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

	if c.Request().Method == http.MethodPost {
		var request viewRequestPostBody
		if err := c.Bind(&request); err != nil {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %s", err.Error()))
		}

		for k, v := range request.Variables {
			opts = append(opts, WithVariable(k, v))
		}
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
