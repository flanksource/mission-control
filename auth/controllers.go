package auth

import (
	"bytes"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/flanksource/commons/duration"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/mail"
	icrbac "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

func RegisterRoutes(e *echo.Echo) {
	// Cannot register this routes in auth/rbac package as it would create a cyclic import
	e.POST("/auth/:id/update_state", UpdateAccountState, icrbac.PersonManage())
	e.POST("/auth/:id/properties", UpdateAccountProperties, icrbac.PersonManage())
	e.GET("/auth/whoami", WhoAmI)
	e.POST("/auth/create_token", CreateToken)
	e.GET("/auth/tokens", ListTokens)
	e.DELETE("/auth/token/:id", DeleteToken)
}

type InviteUserRequest struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`

	// Teams are the names of the teams the person joins when they accept the invite.
	Teams []string `json:"teams,omitempty"`

	// Deprecated: built-in roles were removed, so an invite with a role is rejected.
	// Invites name teams instead (specs/authorization/rolebindings.md, Section 2.2).
	Role string `json:"role,omitempty"`
}

func (k *KratosHandler) InviteUser(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var reqData InviteUserRequest
	if err := c.Bind(&reqData); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}

	smtp, err := mail.GetDefaultSMTPForRequest(ctx)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "error getting SMTP config"))
	}

	invite, err := k.createInvite(ctx, reqData)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	data := map[string]string{
		"firstName": reqData.FirstName,
		"link":      registrationLink(invite.ID.String()),
	}

	var body bytes.Buffer
	if err := inviteUserTemplate.Execute(&body, data); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	inviteMail := mail.New([]string{reqData.Email}, "Invitation to join Mission Control", body.String(), "text/html")
	if err = inviteMail.Send(smtp); err != nil {
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

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{
		Message: "success",
		Payload: map[string]any{
			"user":           user,
			"access":         icrbac.AccessSummary(ctx),
			"access_actions": icrbac.AccessActions(ctx),
			"hostname":       hostname,
		},
	})
}

type CreateTokenRequest struct {
	Name      string `json:"name"`
	Expiry    string `json:"expiry"`
	AutoRenew bool   `json:"auto_renew"`

	// Roles limit the token to what one of their rules also matches, within its owner's access. Each becomes a
	// RoleBinding that selects the token. At least one is required.
	Roles db.AccessTokenRoles `json:"roles"`
}

func CreateToken(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	user := ctx.User()
	if user == nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EUNAUTHORIZED, "error fetching user"))
	}

	// An access token authenticates as the person, without the grants the subject is limited to
	if !icrbac.ActsAsPerson(ctx) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "access tokens can't be created by users of external identity providers"))
	}

	if madeWithAccessToken(ctx) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "access tokens can't be created with an access token"))
	}

	var err error
	var reqData CreateTokenRequest
	if err := c.Bind(&reqData); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}

	if len(reqData.Roles) == 0 {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "a token must list at least one role; list mission-control-builtin/admin and mission-control-builtin/plugin-admin for all of your access"))
	}

	for _, role := range reqData.Roles {
		if role.Namespace == "" || role.Name == "" {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "role %q must have a namespace and a name", role.Namespace+"/"+role.Name))
		}
	}

	var expiry duration.Duration = 0 // Default
	if reqData.Expiry != "" {
		expiry, err = duration.ParseDuration(reqData.Expiry)
		if err != nil {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "error parsing expiry[%s]: %v", reqData.Expiry, err))
		}
	}

	token, _, err := db.CreateAccessTokenForPerson(ctx, user, reqData.Name, time.Duration(expiry), reqData.AutoRenew, reqData.Roles)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "error creating access token"))
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "success", Payload: map[string]string{"token": token.PlainText()}})
}

func ListTokens(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	user := ctx.User()
	if user == nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EUNAUTHORIZED, "error fetching user"))
	}

	if madeWithAccessToken(ctx) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "access tokens can't be listed with an access token"))
	}

	// A person lists their own tokens without a grant. Agents' tokens are listed with the agent.
	payload, err := db.ListAccessTokens(ctx, user.ID)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "unable to list tokens"))
	}

	// A binding's validation message can name its Role's rules and Scopes
	if !icrbac.CheckContext(ctx, adapter.ObjectRoles, policy.ActionRead) {
		for i := range payload {
			for j := range payload[i].RoleBindings {
				payload[i].RoleBindings[j].Error = nil
			}
		}
	}
	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "success", Payload: payload})
}

func DeleteToken(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	user := ctx.User()
	if user == nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EUNAUTHORIZED, "error fetching user"))
	}

	tokenID := c.Param("id")
	if tokenID == "" {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "token id not provided"))
	}

	if madeWithAccessToken(ctx) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "access tokens can't be deleted with an access token"))
	}

	token, err := db.GetAccessToken(ctx, tokenID)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "error fetching token"))
	}

	// A person deletes only their own tokens. Agents' tokens are deleted with the agent, and no action grants
	// deleting another person's (specs/authorization/identities.md, Section 4).
	if token.PersonID != user.ID || lo.FromPtr(token.CreatedBy) != user.ID {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "only the creator can delete an access token"))
	}

	if err := DeleteAccessToken(ctx, tokenID); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "unable to delete token"))
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "success"})
}
