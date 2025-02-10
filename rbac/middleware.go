package rbac

import (
	"errors"
	"net/http"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"
)

var (
	ErrNoUserID          = errors.New("unauthorized. User not found for RBAC")
	ErrAccessDenied      = errors.New("unauthorized. Access Denied")
	ErrMisconfiguredRBAC = errors.New("unauthorized. RBAC policy not configured correctly")
)

type MiddlewareFunc = func(echo.HandlerFunc) echo.HandlerFunc

func Playbook(action string) MiddlewareFunc {
	return Authorization(policy.ObjectPlaybooks, action)
}

func Catalog(action string) MiddlewareFunc {
	return Authorization(policy.ObjectCatalog, action)
}

func Topology(action string) MiddlewareFunc {
	return Authorization(policy.ObjectTopology, action)
}

func Canary(action string) MiddlewareFunc {
	return Authorization(policy.ObjectCanary, action)
}

func DbMiddleware() MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Request().URL.Path
			if !strings.HasPrefix(path, "/db/") {
				return next(c)
			}
			action := rbac.GetActionFromHttpMethod(c.Request().Method)
			resource := strings.ReplaceAll(path, "/db/", "")

			object := rbac.GetObjectByTable(resource)

			if action == "" {
				return c.String(http.StatusForbidden, ErrMisconfiguredRBAC.Error())
			}
			if object == "" {
				return c.String(http.StatusNotFound, "")

			}

			ctx := c.Request().Context().(context.Context)
			user := ctx.User()

			if !rbac.CheckContext(ctx, object, action) {
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
			if rbac.Enforcer() == nil {
				return next(c)
			}

			// If action is unset, extract from HTTP Method
			if action == "" {
				action = rbac.GetActionFromHttpMethod(c.Request().Method)
			}

			ctx := c.Request().Context().(context.Context)
			u := ctx.User()

			if u == nil {
				return c.String(http.StatusUnauthorized, "Not logged in")
			}
			if object == "" || action == "" {
				return c.String(http.StatusForbidden, ErrMisconfiguredRBAC.Error())
			}

			if !rbac.CheckContext(ctx, object, action) {
				c.Response().Header().Add("X-Rbac-Subject", u.ID.String())
				c.Response().Header().Add("X-Rbac-Object", object)
				c.Response().Header().Add("X-Rbac-Action", action)

				return c.String(http.StatusForbidden, ErrAccessDenied.Error())
			}

			return next(c)
		}
	}
}
