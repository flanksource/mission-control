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
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	oryClient "github.com/ory/client-go"
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
	e.POST("/auth/can-i", CanIHandler)
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

	if !oryClient.IdentityState(reqData.State).IsValid() {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid state: %s. Allowed values are %s", reqData.State, oryClient.AllowedIdentityStateEnumValues))
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

	if lo.FromPtr(token.CreatedBy) != ctx.User().ID || !slices.Contains(roles, policy.RoleAdmin) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EUNAUTHORIZED, "only the creator or admins can delete access tokens"))
	}

	if err := db.DeleteAccessToken(ctx, tokenID); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "unable to delete token"))
	}
	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "success"})
}

type CanIRequest struct {
	PlaybookID   *uuid.UUID `json:"playbook_id,omitempty"`
	ConfigID     *uuid.UUID `json:"config_id,omitempty"`
	ComponentID  *uuid.UUID `json:"component_id,omitempty"`
	CheckID      *uuid.UUID `json:"check_id,omitempty"`
	ConnectionID *uuid.UUID `json:"connection_id,omitempty"`
	Actions      []string   `json:"actions"`
}

type CanIResponse struct {
	Response []map[string]bool `json:"response"`
}

func canI(ctx context.Context, userID string, req CanIRequest) (map[string]bool, error) {
	if len(req.Actions) == 0 {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "at least one action is required")
	}

	// Build ABAC attribute from the provided resource IDs
	var attr models.ABACAttribute

	if req.PlaybookID != nil {
		playbook, err := query.FindPlaybook(ctx, req.PlaybookID.String())
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "error fetching playbook")
		}
		if playbook == nil {
			return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "playbook(id=%s) not found", req.PlaybookID)
		}
		attr.Playbook = *playbook
	}

	if req.ConfigID != nil {
		config, err := query.GetCachedConfig(ctx, req.ConfigID.String())
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "error fetching config")
		}
		if config == nil {
			return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "config(id=%s) not found", req.ConfigID)
		}
		attr.Config = *config
	}

	if req.ComponentID != nil {
		component, err := query.GetCachedComponent(ctx, req.ComponentID.String())
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "error fetching component")
		}
		if component == nil {
			return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "component(id=%s) not found", req.ComponentID)
		}
		attr.Component = *component
	}

	if req.CheckID != nil {
		check, err := query.FindCachedCheck(ctx, req.CheckID.String())
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "error fetching check")
		}
		if check == nil {
			return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "check(id=%s) not found", req.CheckID)
		}
		attr.Check = *check
	}

	if req.ConnectionID != nil {
		var connection models.Connection
		if err := ctx.DB().First(&connection, req.ConnectionID).Error; err != nil {
			return nil, ctx.Oops().Wrapf(err, "error fetching connection")
		}
		attr.Connection = connection
	}

	response := make(map[string]bool)

	// Check permissions for each action
	for _, action := range req.Actions {
		allowed := rbac.HasPermission(ctx, userID, &attr, action)
		response[action] = allowed
	}

	return response, nil
}

func CanIHandler(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	user := ctx.User()
	if user == nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EUNAUTHORIZED, "error fetching user"))
	}

	var requests []CanIRequest
	if err := c.Bind(&requests); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}

	if len(requests) == 0 {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "at least one request is required"))
	}

	var responses []map[string]bool

	for _, req := range requests {
		result, err := canI(ctx, user.ID.String(), req)
		if err != nil {
			return dutyAPI.WriteError(c, err)
		}

		responses = append(responses, result)
	}

	return c.JSON(http.StatusOK, CanIResponse{Response: responses})
}
