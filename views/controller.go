package views

import (
	"errors"
	"net/http"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/gomplate/v3"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
	"gorm.io/gorm"

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

	g := e.Group("/view", rbac.ViewRead())
	g.GET("/list", HandleViewList)
	g.GET("/display-plugin-variables/:viewID", GetDisplayPluginsVariables)

	// Returns the full metadata of a view, including child views in viewRef, except the table rows
	g.GET("/metadata/:id", HandleGetViewMetadataByID)

	// Deprecated: Use POST request
	g.GET("/:namespace/:name", GetViewByNamespaceName)
	g.GET("/:id", GetViewByID)

	g.POST("/:namespace/:name", GetViewByNamespaceName)
	g.POST("/:id", GetViewByID)

	e.GET("/dashboard", HandleGetDashboard, rbac.ViewRead())
}

func HandleGetViewMetadataByID(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	idParam := c.Param("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid view id: %s", idParam))
	}

	var view models.View
	if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", id).First(&view).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "view(id=%s) not found", id.String()))
		}
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	response, err := getViewMetadata(ctx, view)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}

// HandleGetDashboard resolves the dashboard view from properties and
// delegates to getViewMetadata.
func HandleGetDashboard(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	dashboardView, err := resolveDashboardView(ctx)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	response, err := getViewMetadata(ctx, *dashboardView)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}

func GetDisplayPluginsVariables(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	viewID := c.Param("viewID")
	configID := c.QueryParam("config_id")
	if configID == "" {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "config_id is required"))
	}

	var view models.View
	if err := ctx.DB().Select("id, namespace, name").Where("id = ? AND deleted_at IS NULL", viewID).First(&view).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "view(id=%s) not found", viewID))
		}
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	} else if !CanRead(ctx, view) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "view(id=%s) not found", viewID))
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
	if err := ctx.DB().Select("id, namespace, name").Where("id = ?", id).Where("deleted_at IS NULL").First(&view).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "view(id=%s) not found", id))
		}
		return dutyAPI.WriteError(c, err)
	}

	if !CanRead(ctx, view) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "view(id=%s) not found", id))
	}

	return getViewByNamespaceName(ctx, c, view.Namespace, view.Name)
}

func GetViewByNamespaceName(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	namespace := c.Param("namespace")
	name := c.Param("name")

	// Fetch the view to check ABAC permissions
	var view models.View
	if err := ctx.DB().Select("id, namespace, name").Where("namespace = ? AND name = ?", namespace, name).Where("deleted_at IS NULL").First(&view).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "view(namespace=%s, name=%s) not found", namespace, name))
		}
		return dutyAPI.WriteError(c, err)
	}

	if !CanRead(ctx, view) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "view(namespace=%s, name=%s) not found", namespace, name))
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
	if err := ctx.DB().Where("id = ?", id).First(&config).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("config(id=%s) not found", id)
		}
		return nil, ctx.Oops("db").Wrap(err)
	}

	list, err := db.FindViewsForConfig(ctx, config)
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	return lo.Filter(list, func(item api.ViewListItem, _ int) bool {
		return CanRead(ctx, models.View{ID: item.ID, Name: item.Name, Namespace: item.Namespace})
	}), nil
}

// CanRead reports whether the subject may read the view, and with it every row and panel of it
// (specs/authorization/roles.md, Section 3.2).
func CanRead(ctx context.Context, view models.View) bool {
	return rbac.HasPermission(ctx, ctx.Subject(), &models.ABACAttribute{View: view}, policy.ActionRead)
}

// CanReadNamed is CanRead for the view of the namespace and name. A view that doesn't exist can't be read.
func CanReadNamed(ctx context.Context, namespace, name string) (bool, error) {
	var view models.View
	if err := ctx.DB().Select("id, namespace, name").Where("namespace = ? AND name = ? AND deleted_at IS NULL", namespace, name).First(&view).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return CanRead(ctx, view), nil
}
