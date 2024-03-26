package connection

import (
	"fmt"
	"net/http"

	"github.com/flanksource/commons/utils"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/api"
)

func TestConnection(c echo.Context) error {
	ctx := c.Request().Context().(context.Context).WithUser(&models.Person{ID: utils.Deref(api.SystemUserID)})

	var id = c.Param("id")

	var connection models.Connection
	if err := ctx.DB().Where("id = ?", id).Find(&connection).Error; err != nil {
		dutyAPI.WriteError(c, err)
	}

	if err := Test(ctx, &connection); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "ok"})
}

func RegisterRoutes(e *echo.Echo) *echo.Group {
	prefix := "connection"
	connectionGroup := e.Group(fmt.Sprintf("/%s", prefix))
	connectionGroup.POST("/test/:id", TestConnection)

	return connectionGroup
}
