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
			action := GetActionFromHttpMethod(c.Request().Method)
			resource := strings.ReplaceAll(path, "/db/", "")

			object := GetObjectByTable(resource)

			if action == "" {
				return c.String(http.StatusForbidden, ErrMisconfiguredRBAC.Error())
			}
			if object == "" {
				return c.String(http.StatusNotFound, "")

			}

			ctx := c.Request().Context().(context.Context)
			user := ctx.User()

			if !CheckContext(ctx, object, action) {
				c.Response().Header().Add("X-Rbac-Subject", user.ID.String())
				c.Response().Header().Add("X-Rbac-Object", object)
				c.Response().Header().Add("X-Rbac-Action", action)

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
			if enforcer == nil {
				return next(c)
			}

			// If action is unset, extract from HTTP Method
			if action == "" {
				action = GetActionFromHttpMethod(c.Request().Method)
			}

			ctx := c.Request().Context().(context.Context)
			u := ctx.User()

			if u == nil {
				return c.String(http.StatusUnauthorized, "Not logged in")
			}
			if object == "" || action == "" {
				return c.String(http.StatusForbidden, ErrMisconfiguredRBAC.Error())
			}

			if !CheckContext(ctx, object, action) {
				c.Response().Header().Add("X-Rbac-Subject", u.ID.String())
				c.Response().Header().Add("X-Rbac-Object", object)
				c.Response().Header().Add("X-Rbac-Action", action)

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

	return Check(ctx, user.ID.String(), object, action)
}

func HasPermission(ctx context.Context, subject string, objects map[string]any, action string) bool {
	if enforcer == nil {
		return true
	}

	allowed, err := enforcer.Enforce(subject, objects, action)
	if err != nil {
		ctx.Errorf("error checking abac for subject=%s action=%s", subject, action)
		return false
	}

	return allowed
}
