package connection

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/utils"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/rbac"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}
func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /connection routes")

	prefix := "connection"
	connectionGroup := e.Group(fmt.Sprintf("/%s", prefix))
	connectionGroup.POST("/test/:id", TestConnection, rbac.Authorization(rbac.ObjectConnection, rbac.ActionUpdate))

}

func TestConnection(c echo.Context) error {
	ctx := c.Request().Context().(context.Context).WithUser(&models.Person{ID: utils.Deref(api.SystemUserID)})

	var id = c.Param("id")

	var connection models.Connection
	if err := ctx.DB().Where("id = ?", id).First(&connection).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "connection was not found"))
		}

		return dutyAPI.WriteError(c, err)
	}

	if err := Test(ctx, &connection); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "ok"})
}
