package auth

import (
	"net/http"
	"time"

	"github.com/flanksource/commons/duration"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"

	"github.com/flanksource/incident-commander/db"
	icrbac "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

// RegisterServiceAccountRoutes registers the routes that manage service accounts and their tokens. Each takes
// service-account:manage, and refuses a request made with an access token or by a user of an external identity
// provider (specs/authorization/service-accounts.md, Section 3).
func RegisterServiceAccountRoutes(e *echo.Echo) {
	group := e.Group("/service-accounts", icrbac.ServiceAccountManage())
	group.GET("", ListServiceAccounts)
	group.POST("", CreateServiceAccount)
	group.GET("/:name", GetServiceAccount)
	group.DELETE("/:name", DeleteServiceAccount)
	group.GET("/:name/tokens", ListServiceAccountTokens)
	group.POST("/:name/tokens", CreateServiceAccountToken)
	group.DELETE("/:name/tokens/:id", RevokeServiceAccountToken)
	group.POST("/:name/tokens/:id/roles", AddServiceAccountTokenRole)
	group.DELETE("/:name/tokens/:id/roles/:roleNamespace/:roleName", RemoveServiceAccountTokenRole)
}

// AddServiceAccountTokenRole binds a token of the service account to a Role.
func AddServiceAccountTokenRole(c echo.Context) error {
	return changeServiceAccountTokenRole(c, true)
}

// RemoveServiceAccountTokenRole removes the binding of a token of the service account to a Role.
func RemoveServiceAccountTokenRole(c echo.Context) error {
	return changeServiceAccountTokenRole(c, false)
}

// changeServiceAccountTokenRole changes a Role of a token of the service account in the route. A token of anyone
// else is not found.
func changeServiceAccountTokenRole(c echo.Context, add bool) error {
	ctx := c.Request().Context().(context.Context)

	account, err := db.FindServiceAccount(ctx, c.Param("name"))
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}
	return changeTokenRole(c, add, func(token models.AccessToken) bool {
		return token.PersonID == account.ID
	})
}

func ListServiceAccounts(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	accounts, err := db.ListServiceAccounts(ctx)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}
	return c.JSON(http.StatusOK, accounts)
}

type CreateServiceAccountRequest struct {
	Name string `json:"name"`
}

// CreateServiceAccount creates a service account. The bindings that already name it take effect before the response.
func CreateServiceAccount(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var req CreateServiceAccountRequest
	if err := c.Bind(&req); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}

	account, err := db.CreateServiceAccount(ctx, req.Name)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	if err := adapter.ReloadPolicy(); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "service account %s was created, but the policy failed to reload", account.Name))
	}
	return c.JSON(http.StatusCreated, account)
}

// ServiceAccountDetail is a service account, with what it may do through the Kubernetes proxy and, for a caller who
// may read RoleBindings, the bindings that name it.
type ServiceAccountDetail struct {
	db.ServiceAccount

	// KubernetesProxy is what the service account holds of kubernetes-proxy:read and kubernetes-proxy:write. A
	// kubeconfig of an account that holds neither is refused on every request.
	KubernetesProxy map[string]bool `json:"kubernetes_proxy"`

	// RoleBindings are omitted unless the caller may read RoleBindings.
	RoleBindings []models.RoleBinding `json:"role_bindings,omitempty"`
}

func GetServiceAccount(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	account, err := db.FindServiceAccount(ctx, c.Param("name"))
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	detail := ServiceAccountDetail{ServiceAccount: *account, KubernetesProxy: map[string]bool{}}
	for _, action := range []string{adapter.ActionKubernetesProxyRead, adapter.ActionKubernetesProxyWrite} {
		detail.KubernetesProxy[action] = rbac.Check(ctx, account.ID.String(), policy.ObjectKubernetesProxy, action)
	}

	if icrbac.CheckContext(ctx, adapter.ObjectRoleBindings, policy.ActionRead) {
		if detail.RoleBindings, err = db.ServiceAccountRoleBindings(ctx, account.Name); err != nil {
			return dutyAPI.WriteError(c, err)
		}
	}
	return c.JSON(http.StatusOK, detail)
}

// DeleteServiceAccount deletes the service account with its tokens and their bindings. The RoleBindings that name it
// are left alone, and select whichever account is created under its name next.
func DeleteServiceAccount(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	account, err := db.FindServiceAccount(ctx, c.Param("name"))
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	var revoked []string
	if err := db.DeleteServiceAccount(ctx, *account, func(tokenIDs []string) {
		revoked = tokenIDs
		for _, id := range tokenIDs {
			deletedTokensCache.Set(id, true, cache.DefaultExpiration)
		}
	}); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	for _, id := range revoked {
		tokenID := uuid.MustParse(id)
		invalidateRLSCacheForToken(tokenID)
		icrbac.ForgetAccessToken(tokenID)
	}

	if err := adapter.ReloadPolicy(); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "service account %s was deleted, but the policy failed to reload", account.Name))
	}
	return c.NoContent(http.StatusNoContent)
}

func ListServiceAccountTokens(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	account, err := db.FindServiceAccount(ctx, c.Param("name"))
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	tokens, err := db.ListServiceAccountTokens(ctx, *account)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	// A binding's validation message can name its Role's rules and Scopes
	if !icrbac.CheckContext(ctx, adapter.ObjectRoles, policy.ActionRead) {
		for i := range tokens {
			for j := range tokens[i].RoleBindings {
				tokens[i].RoleBindings[j].Error = nil
			}
		}
	}
	return c.JSON(http.StatusOK, tokens)
}

type CreateServiceAccountTokenRequest struct {
	Name      string `json:"name"`
	Expiry    string `json:"expiry"`
	AutoRenew bool   `json:"autoRenew"`

	// Roles limit the token to what one of their rules also matches, within the service account's access. At least
	// one is required.
	Roles db.AccessTokenRoles `json:"roles"`
}

// CreatedServiceAccountToken is a token just issued: the only time its value is shown.
type CreatedServiceAccountToken struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	Token     string     `json:"token"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CreateServiceAccountToken issues a token of the service account, usable as soon as it's returned.
func CreateServiceAccountToken(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	account, err := db.FindServiceAccount(ctx, c.Param("name"))
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	var req CreateServiceAccountTokenRequest
	if err := c.Bind(&req); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}

	expiry, err := ParseTokenExpiry(req.Expiry)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	} else if err := ValidateTokenRoles(req.Roles); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	token, created, err := db.CreateServiceAccountToken(ctx, *account, ctx.User().ID, req.Name, expiry, req.AutoRenew, req.Roles)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusCreated, CreatedServiceAccountToken{ID: created.ID, Name: created.Name, Token: token.PlainText(), ExpiresAt: created.ExpiresAt})
}

// RevokeServiceAccountToken deletes a token of the service account and its bindings.
func RevokeServiceAccountToken(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	account, err := db.FindServiceAccount(ctx, c.Param("name"))
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	tokenID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid token id %q", c.Param("id")))
	}

	token, err := db.GetAccessToken(ctx, tokenID.String())
	if err != nil || token.PersonID != account.ID {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "service account %s has no token %s", account.Name, tokenID))
	}

	if err := DeleteAccessToken(ctx, tokenID.String()); err != nil {
		return dutyAPI.WriteError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// ParseTokenExpiry parses the expiry of a token to create, e.g. 30d. Empty is 0, the default expiry.
func ParseTokenExpiry(value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	expiry, err := duration.ParseDuration(value)
	if err != nil {
		return 0, dutyAPI.Errorf(dutyAPI.EINVALID, "error parsing expiry[%s]: %v", value, err)
	} else if expiry <= 0 {
		return 0, dutyAPI.Errorf(dutyAPI.EINVALID, "expiry %s must be positive", value)
	}
	return time.Duration(expiry), nil
}

// ValidateTokenRoles checks the Roles a token is created with: at least one, each named by namespace and name.
func ValidateTokenRoles(roles db.AccessTokenRoles) error {
	if len(roles) == 0 {
		return dutyAPI.Errorf(dutyAPI.EINVALID, "a token must list at least one role; list mission-control-builtin/admin and mission-control-builtin/plugin-admin for all of the access")
	}
	for _, role := range roles {
		if role.Namespace == "" || role.Name == "" {
			return dutyAPI.Errorf(dutyAPI.EINVALID, "role %q must have a namespace and a name", role.Namespace+"/"+role.Name)
		}
	}
	return nil
}
