package rbac

import (
	"net/http"

	"github.com/flanksource/commons/logger"
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
			userID = "018654a9-18b3-1b99-bb41-d975e1fbcc13"
			logger.Infof("user is |%s|", userID)
			if userID == "" {
				return c.String(http.StatusUnauthorized, "Unauthorized")
			}

			if isAdmin, _ := Enforcer.HasRoleForUser(userID, RoleAdmin); isAdmin {
				return next(c)
			}

			if !Check(userID, object, action) {
				return c.String(http.StatusUnauthorized, "Unauthorized")
			}

			return next(c)
		}
	}
}
