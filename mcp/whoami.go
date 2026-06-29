package mcp

import (
	gocontext "context"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const toolWhoami = "whoami"

type whoamiPerson struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Email   string `json:"email,omitempty"`
	Type    string `json:"type,omitempty"`
	Role    string `json:"role,omitempty"`
	IsToken bool   `json:"is_token,omitempty"`
	Subject string `json:"subject,omitempty"`
}

type whoamiResponse struct {
	Subject            string        `json:"subject"`
	Authenticated      *whoamiPerson `json:"authenticated,omitempty"`
	Owner              *whoamiPerson `json:"owner,omitempty"`
	Delegated          bool          `json:"delegated"`
	SubjectRoles       []string      `json:"subject_roles,omitempty"`
	SubjectPermissions []string      `json:"subject_permissions,omitempty"`
	OwnerRoles         []string      `json:"owner_roles,omitempty"`
	OwnerPermissions   []string      `json:"owner_permissions,omitempty"`
	MCPUseAllowed      bool          `json:"mcp_use_allowed"`
}

func whoamiHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	subject := ctx.Subject()
	owner, err := resolveOwner(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	subjectRoles, err := rbac.RolesForUser(subject)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	subjectPermissions, err := rbac.PermsForUser(subject)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	ownerRoles, err := rbac.RolesForUser(owner)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	ownerPermissions, err := rbac.PermsForUser(owner)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	allowed, err := rbac.Enforcer().Enforce(owner, policy.ObjectMCP, policy.ActionMCPUse)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	resp := whoamiResponse{
		Subject:            subject,
		Authenticated:      personForSubject(ctx, subject),
		Owner:              personForSubject(ctx, owner),
		Delegated:          owner != "" && subject != owner,
		SubjectRoles:       subjectRoles,
		SubjectPermissions: permissionStrings(subjectPermissions),
		OwnerRoles:         ownerRoles,
		OwnerPermissions:   permissionStrings(ownerPermissions),
		MCPUseAllowed:      allowed,
	}

	return structToMCPResponse(req, resp), nil
}

func personForSubject(ctx context.Context, subject string) *whoamiPerson {
	if subject == "" {
		return nil
	}

	if user := ctx.User(); user != nil && user.ID.String() == subject {
		return personToWhoami(user, subject)
	}

	id, err := uuid.Parse(subject)
	if err != nil {
		return &whoamiPerson{Subject: subject}
	}

	var person models.Person
	if err := ctx.DB().Where("id = ?", id).First(&person).Error; err != nil {
		return &whoamiPerson{Subject: subject}
	}

	return personToWhoami(&person, subject)
}

func personToWhoami(person *models.Person, subject string) *whoamiPerson {
	if person == nil {
		return nil
	}

	return &whoamiPerson{
		ID:      person.ID.String(),
		Name:    person.Name,
		Email:   person.Email,
		Type:    person.Type,
		Role:    person.Properties.Role,
		IsToken: person.Type == "access_token",
		Subject: subject,
	}
}

func permissionStrings(permissions []policy.Permission) []string {
	items := make([]string, 0, len(permissions))
	for _, p := range permissions {
		items = append(items, p.String())
	}
	return items
}

func registerWhoami(s *server.MCPServer) {
	s.AddTool(mcp.NewTool(toolWhoami,
		mcp.WithDescription("Return the currently authenticated MCP subject, delegated owner, roles, and permissions."),
		mcp.WithReadOnlyHintAnnotation(true)), whoamiHandler)
}
