package rbac

import (
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

// PersonManage authorizes a person operation: inviting, creating, disabling, re-enabling, changing or deleting a person
// (specs/authorization/roles.md, Section 2.3).
func PersonManage() MiddlewareFunc {
	return Authorization(policy.ObjectPeople, adapter.ActionPersonManage)
}

// ServiceAccountManage authorizes managing service accounts and their tokens. It refuses a request made with an
// access token or by a user of an external identity provider, whatever their grants
// (specs/authorization/service-accounts.md, Section 3).
func ServiceAccountManage() MiddlewareFunc {
	authorized := Authorization(adapter.ObjectServiceAccounts, adapter.ActionServiceAccountManage)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		checked := authorized(next)
		return func(c echo.Context) error {
			if err := ServiceAccountManageRefusal(c.Request().Context().(context.Context)); err != nil {
				return dutyAPI.WriteError(c, err)
			}
			return checked(c)
		}
	}
}

// ServiceAccountManageRefusal returns why the request may never use service-account:manage, or nil when only its
// grants decide.
func ServiceAccountManageRefusal(ctx context.Context) error {
	if MadeWithAccessToken(ctx) {
		return dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "service accounts can't be managed with an access token")
	}
	if user := ctx.User(); user != nil && user.Type == adapter.PersonTypeFederated {
		return dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "service accounts can't be managed by users of external identity providers")
	}
	return nil
}

// CanManageServiceAccounts reports whether the request may use service-account:manage.
func CanManageServiceAccounts(ctx context.Context) bool {
	return ServiceAccountManageRefusal(ctx) == nil && CheckContext(ctx, adapter.ObjectServiceAccounts, adapter.ActionServiceAccountManage)
}
