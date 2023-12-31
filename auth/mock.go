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
func MockMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		name, _, ok := c.Request().BasicAuth()
		if !ok {
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}
		ctx := c.Request().Context().(context.Context)

		var person models.Person
		if err := ctx.DB().Where("name = ? or email = ?", name, name).First(&person).Error; err != nil {
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}

		ctx = ctx.WithUser(&models.Person{ID: person.ID, Email: person.Email})
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}
