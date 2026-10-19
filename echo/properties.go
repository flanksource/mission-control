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
	group.POST("", SaveProperty)
	group.GET("/*", GetProperty)
	group.PUT("/*", SaveProperty)
	group.DELETE("/*", DeleteProperty)
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
	name := propertyName(c)
	if name == "" {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "name is required"))
	}
	var property models.AppProperty
	if err := ctx.DB().Where("name = ?", name).First(&property).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "property was not found"))
		}
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}
	return c.JSON(http.StatusOK, property)
}

// SaveProperty creates on POST and upserts on PUT, recording the authenticated creator on insert.
func SaveProperty(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)
	var body struct {
		Name  string  `json:"name"`
		Value *string `json:"value"`
	}
	if err := c.Bind(&body); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}
	name := body.Name
	if c.Request().Method == http.MethodPut {
		name = propertyName(c)
	}
	if strings.TrimSpace(name) == "" || body.Value == nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "name and value are required"))
	}

	var property models.AppProperty
	if user := ctx.User(); user != nil {
		property.CreatedBy = &user.ID
	}
	query := "INSERT INTO properties (name, value, created_by) VALUES (?, ?, ?) ON CONFLICT (name) "
	if c.Request().Method == http.MethodPut {
		query += "DO UPDATE SET value = excluded.value, updated_at = now(), deleted_at = NULL"
	} else {
		query += "DO NOTHING"
	}
	result := ctx.DB().Raw(query+" RETURNING *", name, *body.Value, property.CreatedBy).Scan(&property)
	if result.Error != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(result.Error))
	}
	if result.RowsAffected == 0 {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ECONFLICT, "property already exists"))
	}
	ctx.ClearCache()
	return c.JSON(http.StatusOK, property)
}

// DeleteProperty removes the stored override, leaving local properties unchanged.
func DeleteProperty(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)
	name := propertyName(c)
	if name == "" {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "name is required"))
	}
	if err := ctx.DB().Where("name = ?", name).Delete(&models.AppProperty{}).Error; err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}
	ctx.ClearCache()
	return c.NoContent(http.StatusNoContent)
}

// propertyName uses the already-decoded URL path: Echo params preserve encoded slashes,
// and decoding them again would confuse a literal "%2F" with "/".
func propertyName(c echov4.Context) string {
	return strings.TrimPrefix(c.Request().URL.Path, "/property/")
}
