package aditya

import (
	"net/http"

	"github.com/labstack/echo/v4"

	echoSrv "github.com/flanksource/incident-commander/echo"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	e.GET("/aditya", HelloWorld)
}

func HelloWorld(c echo.Context) error {
	return c.String(http.StatusOK, "hello world")
}
