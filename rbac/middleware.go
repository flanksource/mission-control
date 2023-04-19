package rbac

import (
	"net/http"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/auth"
	"github.com/labstack/echo/v4"
)

func Authorization(object, action string) func(echo.HandlerFunc) echo.HandlerFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			userID := c.Request().Header.Get(auth.UserIDHeaderKey)
			if userID == "" {
				return c.String(http.StatusUnauthorized, "Unauthorized. User not found for RBAC")
			}

			// Everyone with an account is a viewer
			if action == ActionRead && Check(RoleViewer, object, action) {
				return next(c)
			}

			if isAdmin, _ := Enforcer.HasRoleForUser(userID, RoleAdmin); isAdmin {
				return next(c)
			}

			// Database action is defined via HTTP Verb and Path
			if path := c.Request().URL.Path; strings.HasPrefix(path, "/db") {
				action = policyActionFromHTTPMethod(c.Request().Method)
				resource := strings.ReplaceAll(path, "/db/", "")

				// TODO: Use Contains in list than switch
				object = postgrestDatabaseObject(resource)

				isUserViewer, _ := Enforcer.HasRoleForUser(userID, RoleViewer)
				if action == ActionRead && isUserViewer {
					return next(c)
				}

				if object == "" || action == "" {
					logger.Debugf("Skipping RBAC since no rules are defined on table: %s", resource)
					return next(c)
				}
			}

			if object == "" || action == "" {
				return c.String(http.StatusUnauthorized, "Unauthorized. Check role policy")
			}

			if !Check(userID, object, action) {
				return c.String(http.StatusUnauthorized, "Unauthorized. Check role policy")
			}

			return next(c)
		}
	}
}
