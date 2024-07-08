package rbac

import (
	"errors"
	"net/http"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/labstack/echo/v4"
)

var (
	errNoUserID          = errors.New("unauthorized. User not found for RBAC")
	errAccessDenied      = errors.New("unauthorized. Access Denied")
	errMisconfiguredRBAC = errors.New("unauthorized. RBAC policy not configured correctly")
)

type MiddlewareFunc = func(echo.HandlerFunc) echo.HandlerFunc

func Playbook(action string) MiddlewareFunc {
	return Authorization(ObjectPlaybooks, action)
}

func Catalog(action string) MiddlewareFunc {
	return Authorization(ObjectCatalog, action)
}

func Topology(action string) MiddlewareFunc {
	return Authorization(ObjectTopology, action)
}

func Canary(action string) MiddlewareFunc {
	return Authorization(ObjectCanary, action)
}

func DbMiddleware() MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Request().URL.Path
			if !strings.HasPrefix(path, "/db/") {
				return next(c)
			}
			action := policyActionFromHTTPMethod(c.Request().Method)
			resource := strings.ReplaceAll(path, "/db/", "")

			object := postgrestDatabaseObject(resource)

			if object == "" || action == "" {
				return c.String(http.StatusForbidden, errMisconfiguredRBAC.Error())
			}

			if !CheckContext(c, object, action) {
				return c.String(http.StatusForbidden, errAccessDenied.Error())
			}

			return next(c)
		}
	}
}

func Authorization(object, action string) MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip auth if Enforcer is not initialized
			if Enforcer == nil {
				return next(c)
			}
			// Everyone with an account is a viewer
			if action == ActionRead && Check(RoleViewer, object, action) {
				return next(c)
			}

			if object == "" || action == "" {
				return c.String(http.StatusUnauthorized, errMisconfiguredRBAC.Error())
			}

			if !CheckContext(c, object, action) {
				return c.String(http.StatusForbidden, errAccessDenied.Error())
			}

			return next(c)
		}
	}
}

func CheckContext(c echo.Context, object, action string) bool {
	userID := c.Request().Header.Get(api.UserIDHeaderKey)
	if userID == "" {
		return false
	}

	allowed, err := Enforcer.Enforce(userID, object, action)
	if err != nil {
		logger.Errorf("RBAC Enforce failed: %v", err)
	}
	return allowed
}
