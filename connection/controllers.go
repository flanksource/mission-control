package connection

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/rbac"
)

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /connection routes")

	prefix := "connection"
	connectionGroup := e.Group(fmt.Sprintf("/%s", prefix))
	// Acts with a connection's credentials, so it checks connection:use on it, not on the whole connection type
	connectionGroup.POST("/test/:id", TestConnection)

	// The PostgREST roles can't read or write connections, so they're only written through these routes
	connectionGroup.POST("", CreateConnection, rbac.Authorization(policy.ObjectConnection, policy.ActionCreate))
	connectionGroup.GET("/:id", GetConnection)
	connectionGroup.PUT("/:id", SaveConnection)
	connectionGroup.DELETE("/:id", DeleteConnection)

	registerLLMRoutes(e)
}

// TestConnection makes an authenticated call with the connection's credentials, so it requires connection:use on it.
func TestConnection(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var id = c.Param("id")

	var connection models.Connection
	if err := ctx.DB().Where("id = ?", id).First(&connection).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "connection was not found"))
		}

		return dutyAPI.WriteError(c, err)
	}

	if err := rbac.AuthorizeConnectionUse(ctx, connection); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	hydrated, err := ctx.HydrateConnection(&connection)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	result, err := Test(ctx, hydrated)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}
	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "ok", Payload: result})
}
