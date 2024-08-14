package auth

import (
	"net/http"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/labstack/echo/v4"
)

// mockAuthMiddleware doesn't actually authenticate since we never store auth data.
// It simply ensures that the requested user exists in the DB and then attaches the
// users's ID to the context.
func MockAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context().(context.Context)
		logger := ctx.Logger.Named("auth")
		name, _, ok := c.Request().BasicAuth()
		if !ok {
			logger.Warnf("no basic authentication")
			return next(c)
		}

		var person models.Person
		if err := ctx.DB().Where("name = ? or email = ?", name, name).First(&person).Error; err != nil {
			logger.Warnf("user %s not found", name)
			return c.String(http.StatusUnauthorized, "Unauthorized - User not found")
		}

		ctx = ctx.WithUser(&person)

		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}
