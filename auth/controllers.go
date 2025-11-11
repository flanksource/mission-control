package auth

import (
	"bytes"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/flanksource/commons/duration"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/mail"
	icrbac "github.com/flanksource/incident-commander/rbac"
)

func RegisterRoutes(e *echo.Echo) {
	// Cannot register this routes in auth/rbac package as it would create a cyclic import
	e.POST("/auth/:id/update_state", UpdateAccountState)
	e.POST("/auth/:id/properties", UpdateAccountProperties)
	e.GET("/auth/whoami", WhoAmI)
	e.POST("/auth/create_token", CreateToken)
	e.GET("/auth/tokens", ListTokens)
	e.DELETE("/auth/token/:id", DeleteToken)
}

type InviteUserRequest struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	Role      string `json:"role"`
}

func (k *KratosHandler) InviteUser(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var reqData InviteUserRequest
	if err := c.Bind(&reqData); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}

	identity, err := k.createUser(ctx, reqData.FirstName, reqData.LastName, reqData.Email)
	if err != nil {
		// User already exists
		if strings.Contains(err.Error(), http.StatusText(http.StatusConflict)) {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ECONFLICT, "user already exists"))
		}

		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "error creating user"))
	}

	if reqData.Role != "" {
		if err := rbac.AddRoleForUser(identity.Id, reqData.Role); err != nil {
			ctx.Logger.Errorf("failed to add role to user: %v", err)
		}
	}

	recoveryCode, recoveryLink, err := k.createRecoveryLink(ctx, identity.Id)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "error creating recovery link"))
	}

	data := map[string]string{
		"firstName": reqData.FirstName,
		"link":      recoveryLink,
		"code":      recoveryCode,
	}

	var body bytes.Buffer
	if err := inviteUserTemplate.Execute(&body, data); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	inviteMail := mail.New([]string{reqData.Email}, "User Invite", body.String(), "text/html")
	if err = inviteMail.Send(); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "error sending email"))
	}

	delete(data, "firstName")
	return c.JSON(http.StatusOK, data)
}

func UpdateAccountState(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var reqData struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	if err := c.Bind(&reqData); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}

	if !slices.Contains(AllowedIdentityStates, reqData.State) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid state: %s. Allowed values are %v", reqData.State, AllowedIdentityStates))
	}

	if err := db.UpdateIdentityState(ctx, reqData.ID, reqData.State); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "success"})
}

func UpdateAccountProperties(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var props api.PersonProperties
	if err := c.Bind(&props); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}

	err := db.UpdateUserProperties(ctx, c.Param("id"), props)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "success"})
}

func WhoAmI(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	user := ctx.User()
	if user == nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EUNAUTHORIZED, "error fetching user"))
	}

	hostname, _ := os.Hostname()

	roles, err := rbac.RolesForUser(user.ID.String())
	if err != nil {
		ctx.Warnf("Error getting roles: %v", err)
	}
	permissions, err := rbac.PermsForUser(user.ID.String())
	if err != nil {
		ctx.Warnf("Error getting permissions: %v", err)
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{
		Message: "success",
		Payload: map[string]any{
			"user":        user,
			"roles":       roles,
			"permissions": permissions,
			"hostname":    hostname,
		},
	})
}

type CreateTokenRequest struct {
	Name      string `json:"name"`
	Expiry    string `json:"expiry"`
	AutoRenew bool   `json:"auto_renew"`

	// Empty scope means all permissions the user has
	Scope []policy.Permission `json:"scope"`
}

func CreateToken(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	user := ctx.User()
	if user == nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EUNAUTHORIZED, "error fetching user"))
	}

	var err error
	var reqData CreateTokenRequest
	if err := c.Bind(&reqData); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}

	var expiry duration.Duration = 0 // Default
	if reqData.Expiry != "" {
		expiry, err = duration.ParseDuration(reqData.Expiry)
		if err != nil {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "error parsing expiry[%s]: %v", reqData.Expiry, err))
		}
	}

	tokenResult, err := CreateAccessTokenForPerson(ctx, ctx.User(), reqData.Name, time.Duration(expiry), reqData.AutoRenew)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "error creating access token"))
	}

	if _, err := rbac.Enforcer().AddGroupingPolicy(tokenResult.Person.ID.String(), user.ID.String()); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "error grouping token with user"))

	}

	if len(reqData.Scope) > 0 {
		// Remove subject from scope if exists
		for i := range reqData.Scope {
			reqData.Scope[i].Subject = ""
		}

		var permsToDeny [][]string
		diff, _ := lo.Difference(icrbac.AllPermissions, reqData.Scope)
		for _, p := range diff {
			p.Deny = true
			permsToDeny = append(permsToDeny, p.ToArgsWithoutSubject())
		}

		if len(permsToDeny) > 0 {
			if _, err := rbac.Enforcer().AddPermissionsForUser(tokenResult.Person.ID.String(), permsToDeny...); err != nil {
				return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "unable to create token"))
			}
		}
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "success", Payload: map[string]string{"token": tokenResult.Token}})
}

func ListTokens(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	payload, err := db.ListAccessTokens(ctx)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "unable to list tokens"))
	}
	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "success", Payload: payload})
}

func DeleteToken(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	tokenID := c.Param("id")
	if tokenID == "" {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "token id not provided"))
	}

	token, err := db.GetAccessToken(ctx, tokenID)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "error fetching token"))
	}
	roles, err := rbac.RolesForUser(ctx.User().ID.String())
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "error fetching existing roles for user"))
	}

	if lo.FromPtr(token.CreatedBy) != ctx.User().ID && !slices.Contains(roles, policy.RoleAdmin) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "only the creator or admins can delete access tokens"))
	}

	if err := DeleteAccessToken(ctx, tokenID); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "unable to delete token"))
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "success"})
}
