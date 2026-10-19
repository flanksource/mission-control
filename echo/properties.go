package echo

import (
	"errors"
	"net/http"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	dutyEcho "github.com/flanksource/duty/echo"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	echov4 "github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
)

func init() {
	RegisterRoutes(RegisterPropertyRoutes)
}

// RegisterPropertyRoutes keeps stored configuration behind property:manage, outside PostgREST.
func RegisterPropertyRoutes(e *echov4.Echo) {
	e.GET("/properties", func(c echov4.Context) error {
		ctx := c.Request().Context().(context.Context)
		if rbac.CheckContext(ctx, policy.ObjectProperties, policy.ActionPropertyManage) {
			return dutyEcho.Properties(c)
		}
		return dutyEcho.FeatureFlags(c)
	})

	group := e.Group("/property", rbac.Authorization(policy.ObjectProperties, policy.ActionPropertyManage))
	group.GET("", ListProperties)
	group.GET("/:name", GetProperty)
	group.PUT("/:name", SaveProperty)
	group.DELETE("/:name", DeleteProperty)
}

// ListProperties returns stored properties, without local overrides.
func ListProperties(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)
	properties, err := db.GetProperties(ctx)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}
	return c.JSON(http.StatusOK, properties)
}

// GetProperty returns the stored value for editing, even when a local property overrides it.
func GetProperty(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)
	var property models.AppProperty
	if err := ctx.DB().Where("name = ?", c.Param("name")).First(&property).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "property was not found"))
		}
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}
	return c.JSON(http.StatusOK, property)
}

// SaveProperty creates or updates a value, recording the authenticated creator on insert.
func SaveProperty(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)
	name := c.Param("name")
	var body struct {
		Value *string `json:"value"`
	}
	if err := c.Bind(&body); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}
	if strings.TrimSpace(name) == "" || body.Value == nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "name and value are required"))
	}

	var property models.AppProperty
	if user := ctx.User(); user != nil {
		property.CreatedBy = &user.ID
	}
	if err := ctx.DB().Raw(`INSERT INTO properties (name, value, created_by) VALUES (?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET value = excluded.value, updated_at = now(), deleted_at = NULL
		RETURNING *`, name, *body.Value, property.CreatedBy).Scan(&property).Error; err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}
	ctx.ClearCache()
	return c.JSON(http.StatusOK, property)
}

// DeleteProperty removes the stored override, leaving local properties unchanged.
func DeleteProperty(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)
	if err := ctx.DB().Where("name = ?", c.Param("name")).Delete(&models.AppProperty{}).Error; err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}
	ctx.ClearCache()
	return c.NoContent(http.StatusNoContent)
}
