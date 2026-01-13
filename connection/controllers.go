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

	"github.com/flanksource/incident-commander/db"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /connection routes")

	prefix := "connection"
	connectionGroup := e.Group(fmt.Sprintf("/%s", prefix))
	connectionGroup.POST("/test/:id", TestConnection, rbac.Authorization(policy.ObjectConnection, policy.ActionUpdate))
	connectionGroup.GET("/llm", GetDefaultLLMProviderConnection, rbac.Authorization(policy.ObjectConnection, policy.ActionRead))
}

// GetDefaultLLMProviderConnection returns the hydrated connection for the default LLM Provider connection.
//
// It must only be used by the Next Backend for the AI Chat
func GetDefaultLLMProviderConnection(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	conn, err := db.FindDefaultLLMProviderConnection(ctx)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	hydratedConn, err := ctx.HydrateConnection(conn)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, hydratedConn)
}

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

	payload, err := Test(ctx, &connection)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}
	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "ok", Payload: payload})
}
