package mcp

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/samber/lo"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	icrbac "github.com/flanksource/incident-commander/rbac"
)

const (
	toolWhoami    = "whoami"
	toolGetPerson = "get_person"
)

// The ways a caller is signed in, as whoami reports them.
const (
	loginOIDC           = "oidc"
	loginSession        = "session"
	loginAccessToken    = "access_token"
	loginServiceAccount = "service_account"
)

type whoamiPerson struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email,omitempty"`
	Type  string    `json:"type,omitempty"`
}

type whoamiAccessToken struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	AutoRenew bool       `json:"auto_renew"`

	// RoleBindings limit what the token may do to what they also grant, within its owner's access.
	RoleBindings []string `json:"role_bindings"`
}

type whoamiLogin struct {
	Method      string             `json:"method"`
	AccessToken *whoamiAccessToken `json:"access_token,omitempty"`
}

type whoamiResponse struct {
	Person whoamiPerson `json:"person"`
	Login  whoamiLogin  `json:"login"`

	// RoleBindings grant the caller, directly or through their teams, as namespace/name.
	RoleBindings []string `json:"role_bindings"`

	// Actions are the actions the caller may perform on a whole object or without a resource.
	Actions []string `json:"actions"`

	// Access is, by resource type and action, whether the caller may act on all, some or none of the type.
	Access map[string]map[string]icrbac.Access `json:"access"`
}

// roleBindings returns the RoleBindings the principal is grouped under, as namespace/name.
func roleBindings(principal string) ([]string, error) {
	roles, err := dutyRBAC.RolesForUser(principal)
	if err != nil {
		return nil, err
	}
	bindings := lo.FilterMap(roles, func(role string, _ int) (string, bool) {
		return strings.CutPrefix(role, models.BindingPrincipalPrefix)
	})
	bindings = lo.Uniq(bindings)
	slices.Sort(bindings)
	return bindings, nil
}

func whoamiHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	user := ctx.User()
	if user == nil {
		return mcp.NewToolResultError("not authenticated"), nil
	}

	response := whoamiResponse{
		Person:  whoamiPerson{ID: user.ID, Name: user.Name, Email: user.Email, Type: user.Type},
		Login:   whoamiLogin{Method: loginSession},
		Actions: []string{},
		Access:  icrbac.AccessSummary(ctx),
	}

	if tokenID := icrbac.RequestAccessTokenID(ctx); tokenID != nil {
		response.Login.Method = lo.Ternary(user.Type == db.PersonTypeServiceAccount, loginServiceAccount, loginAccessToken)

		var token models.AccessToken
		if err := ctx.DB().Select("id", "name", "created_at", "expires_at", "auto_renew").Where("id = ?", *tokenID).First(&token).Error; err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		response.Login.AccessToken = &whoamiAccessToken{ID: token.ID, Name: token.Name, CreatedAt: token.CreatedAt, ExpiresAt: token.ExpiresAt, AutoRenew: token.AutoRenew}
		if response.Login.AccessToken.RoleBindings, err = roleBindings(models.AccessTokenPrincipal(token.ID)); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
	} else if auth.MadeWithOIDCToken(ctx) {
		response.Login.Method = loginOIDC
	}

	if response.RoleBindings, err = roleBindings(ctx.Subject()); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	for action, allowed := range icrbac.AccessActions(ctx) {
		if allowed {
			response.Actions = append(response.Actions, action)
		}
	}
	slices.Sort(response.Actions)

	out, err := json.Marshal(response)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(out)), nil
}

type personResponse struct {
	ID           uuid.UUID               `json:"id"`
	Name         string                  `json:"name"`
	Email        string                  `json:"email,omitempty"`
	Type         string                  `json:"type,omitempty"`
	Avatar       string                  `json:"avatar,omitempty"`
	Title        string                  `json:"title,omitempty"`
	Organization string                  `json:"organization,omitempty"`
	Phone        string                  `json:"phone,omitempty"`
	ExternalID   string                  `json:"external_id,omitempty"`
	Properties   models.PersonProperties `json:"properties,omitempty"`
	CreatedAt    *time.Time              `json:"created_at,omitempty"`
	UpdatedAt    *time.Time              `json:"updated_at,omitempty"`
	DeletedAt    *time.Time              `json:"deleted_at,omitempty"`
	LastLogin    *time.Time              `json:"last_login,omitempty"`
}

func getPersonHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if !icrbac.CheckContext(ctx, policy.ObjectPeople, policy.ActionRead) {
		return mcp.NewToolResultError(fmt.Sprintf("forbidden: %s does not have %s permission on %s", ctx.Subject(), policy.ActionRead, policy.ObjectPeople)), nil
	}

	id, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if _, err := uuid.Parse(id); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid person id %q", id)), nil
	}

	// A deleted person is found too, as objects keep the ids of the people who created them
	var person personResponse
	if err := ctx.DB().Table("people").
		Select("id, name, COALESCE(email, '') AS email, COALESCE(type, '') AS type, COALESCE(avatar, '') AS avatar, "+
			"COALESCE(title, '') AS title, COALESCE(organization, '') AS organization, COALESCE(phone, '') AS phone, "+
			"COALESCE(external_id, '') AS external_id, properties, created_at, updated_at, deleted_at, last_login").
		Where("id = ?", id).Take(&person).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("person %s not found", id)), nil
		}
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, person), nil
}

func registerPeople(s *server.MCPServer) {
	s.AddTool(mcp.NewTool(toolWhoami,
		mcp.WithDescription("Show who the caller is signed in as: their person (id, name, email, type), how they're signed in "+
			"(oidc: a token of Mission Control's OIDC provider; session: a login session or basic auth; access_token: a personal access token; "+
			"service_account: a service account's access token), the access token's name, expiry and the role bindings that limit it, "+
			"the role bindings that grant the caller, and what they may do: the actions they hold and, by resource type and action, "+
			"whether they may act on all, some or none of it."),
		mcp.WithReadOnlyHintAnnotation(true),
	), whoamiHandler)

	s.AddTool(mcp.NewTool(toolGetPerson,
		mcp.WithDescription("Get a Mission Control person by id, e.g. the created_by of an RBAC object, including people who were deleted."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("id", mcp.Required(), mcp.Description("UUID of the person")),
	), getPersonHandler)
}
