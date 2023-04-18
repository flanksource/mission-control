package rbac

import (
	"net/http"

	"github.com/flanksource/incident-commander/auth"
	"github.com/labstack/echo/v4"
)

func Authorization(object, action string) func(echo.HandlerFunc) echo.HandlerFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// If either are empty, Authorization is disabled
			if object == "" || action == "" {
				return next(c)
			}
			userID := c.Request().Header.Get(auth.UserIDHeaderKey)
			if userID == "" {
				return c.String(http.StatusUnauthorized, "Unauthorized")
			}

			if isAdmin, _ := Enforcer.HasRoleForUser(userID, RoleAdmin); isAdmin {
				return next(c)
			}

			// Database action is defined via HTTP Verb
			if object == ObjectDatabase {
				if c.Request().Method == http.MethodGet {
					action = ActionRead
				} else {
					action = ActionWrite
				}
			}

			if !Check(userID, object, action) {
				return c.String(http.StatusUnauthorized, "Unauthorized")
			}

			return next(c)
		}
	}
}
