package rbac

import (
	"errors"
	"net/http"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/labstack/echo/v4"
)

var (
	ErrNoUserID          = errors.New("unauthorized. User not found for RBAC")
	ErrAccessDenied      = errors.New("unauthorized. Access Denied")
	ErrMisconfiguredRBAC = errors.New("unauthorized. RBAC policy not configured correctly")
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
				return c.String(http.StatusForbidden, ErrMisconfiguredRBAC.Error())
			}

			ctx := c.Request().Context().(context.Context)

			if !CheckContext(ctx, object, action) {
				return c.String(http.StatusForbidden, ErrAccessDenied.Error())
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
			if action == "*" {
				action = policyActionFromHTTPMethod(c.Request().Method)
			}

			ctx := c.Request().Context().(context.Context)

			if object == "" || action == "" {
				return c.String(http.StatusForbidden, ErrMisconfiguredRBAC.Error())
			}

			if !CheckContext(ctx, object, action) {
				return c.String(http.StatusForbidden, ErrAccessDenied.Error())
			}

			return next(c)
		}
	}
}

func CheckContext(ctx context.Context, object, action string) bool {

	user := ctx.User()
	if user == nil {
		return false
	}

	// Everyone with an account is a viewer
	if action == ActionRead && Check(ctx, RoleViewer, object, action) {
		return true
	}

	allowed := Check(ctx, user.Name, object, action)
	return allowed
}
