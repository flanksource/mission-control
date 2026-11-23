package mcp

import (
	gocontext "context"
	"fmt"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/flanksource/incident-commander/db"
	icrbac "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

const (
	toolListTeams        = "list_teams"
	toolListTeamMembers  = "list_team_members"
	toolListPersonTeams  = "list_person_teams"
	toolAddTeamMembers   = "add_team_members"
	toolRemoveTeamMember = "remove_team_member"
)

type teamResponse struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Icon    string    `json:"icon,omitempty"`
	Members int       `json:"members"`
}

type teamMemberResponse struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Email  string    `json:"email,omitempty"`
	Source string    `json:"source,omitempty"`
}

type personTeamResponse struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Source string    `json:"source,omitempty"`
}

// requireTeams checks the action on teams, as the /teams routes do.
func requireTeams(ctx context.Context, action string) error {
	if !icrbac.CheckContext(ctx, adapter.ObjectTeams, action) {
		return fmt.Errorf("forbidden: %s does not have %s permission on %s", ctx.Subject(), action, adapter.ObjectTeams)
	}
	return nil
}

// findTeamID finds a team by id or name.
func findTeamID(ctx context.Context, ref string) (uuid.UUID, error) {
	query := ctx.DB().Model(&models.Team{}).Where("deleted_at IS NULL")
	if id, err := uuid.Parse(ref); err == nil {
		query = query.Where("id = ?", id)
	} else {
		query = query.Where("name = ?", ref)
	}

	var ids []uuid.UUID
	if err := query.Limit(2).Pluck("id", &ids).Error; err != nil {
		return uuid.Nil, ctx.Oops().Wrapf(err, "failed to find team %s", ref)
	}
	switch len(ids) {
	case 0:
		return uuid.Nil, api.Errorf(api.ENOTFOUND, "team %s not found", ref)
	case 1:
		return ids[0], nil
	default:
		return uuid.Nil, api.Errorf(api.EINVALID, "more than one team is named %s: use its id", ref)
	}
}

// findPersonID finds a person by id or email.
func findPersonID(ctx context.Context, ref string) (uuid.UUID, error) {
	query := ctx.DB().Model(&models.Person{}).Where("deleted_at IS NULL")
	if id, err := uuid.Parse(ref); err == nil {
		query = query.Where("id = ?", id)
	} else {
		query = query.Where("LOWER(email) = LOWER(?)", ref)
	}

	var ids []uuid.UUID
	if err := query.Limit(2).Pluck("id", &ids).Error; err != nil {
		return uuid.Nil, ctx.Oops().Wrapf(err, "failed to find person %s", ref)
	}
	switch len(ids) {
	case 0:
		return uuid.Nil, api.Errorf(api.ENOTFOUND, "person %s not found", ref)
	case 1:
		return ids[0], nil
	default:
		return uuid.Nil, api.Errorf(api.EINVALID, "more than one person has the email %s: use their id", ref)
	}
}

func listTeamsHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := requireTeams(ctx, policy.ActionRead); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	query := ctx.DB().Table("teams").
		Select(`teams.id, teams.name, COALESCE(teams.icon, '') AS icon,
			(SELECT COUNT(*) FROM team_members JOIN people ON people.id = team_members.person_id
				WHERE team_members.team_id = teams.id AND people.deleted_at IS NULL) AS members`).
		Where("teams.deleted_at IS NULL").
		Order("teams.name")
	if search := req.GetString("search", ""); search != "" {
		query = query.Where("teams.name ILIKE ?", "%"+search+"%")
	}

	var teams []teamResponse
	if err := query.Scan(&teams).Error; err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, teams), nil
}

func listTeamMembersHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := requireTeams(ctx, policy.ActionRead); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	ref, err := req.RequireString("team")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	teamID, err := findTeamID(ctx, ref)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var members []teamMemberResponse
	if err := ctx.DB().Table("team_members").
		Select("people.id, people.name, COALESCE(people.email, '') AS email, COALESCE(team_members.source, '') AS source").
		Joins("JOIN people ON people.id = team_members.person_id").
		Where("team_members.team_id = ? AND people.deleted_at IS NULL", teamID).
		Order("people.name").
		Scan(&members).Error; err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, members), nil
}

func listPersonTeamsHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := requireTeams(ctx, policy.ActionRead); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	ref, err := req.RequireString("person")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	personID, err := findPersonID(ctx, ref)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var teams []personTeamResponse
	if err := ctx.DB().Table("team_members").
		Select("teams.id, teams.name, COALESCE(team_members.source, '') AS source").
		Joins("JOIN teams ON teams.id = team_members.team_id").
		Where("team_members.person_id = ? AND teams.deleted_at IS NULL", personID).
		Order("teams.name").
		Scan(&teams).Error; err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, teams), nil
}

func addTeamMembersHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := requireTeams(ctx, policy.ActionUpdate); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	ref, err := req.RequireString("team")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	people, err := req.RequireStringSlice("people")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(people) == 0 {
		return mcp.NewToolResultError("people must name at least one person"), nil
	}

	teamID, err := findTeamID(ctx, ref)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	personIDs := make([]uuid.UUID, len(people))
	for i, person := range people {
		if personIDs[i], err = findPersonID(ctx, person); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
	}

	if err := db.AddTeamMembers(ctx, teamID, personIDs); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("added %d member(s) to team %s", len(personIDs), ref)), nil
}

func removeTeamMemberHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := requireTeams(ctx, policy.ActionUpdate); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	teamRef, err := req.RequireString("team")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	personRef, err := req.RequireString("person")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	teamID, err := findTeamID(ctx, teamRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	personID, err := findPersonID(ctx, personRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := db.RemoveTeamMember(ctx, teamID, personID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("removed %s from team %s", personRef, teamRef)), nil
}

func registerTeams(s *server.MCPServer) {
	team := mcp.WithString("team", mcp.Required(), mcp.Description("Name or UUID of the team"))
	person := mcp.WithString("person", mcp.Required(), mcp.Description("Email or UUID of the person"))

	s.AddTool(mcp.NewTool(toolListTeams,
		mcp.WithDescription("List teams with their number of members."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("search", mcp.Description("Only list teams whose name contains this, case-insensitive")),
	), listTeamsHandler)

	s.AddTool(mcp.NewTool(toolListTeamMembers,
		mcp.WithDescription("List the members of a team. Each member's source says how they were added: "+
			"members whose source is team-mapper or starts with provider/ are managed by a team mapper or an identity provider and can't be removed with remove_team_member."),
		mcp.WithReadOnlyHintAnnotation(true),
		team,
	), listTeamMembersHandler)

	s.AddTool(mcp.NewTool(toolListPersonTeams,
		mcp.WithDescription("List the teams a person is a member of, with how each membership was added (source)."),
		mcp.WithReadOnlyHintAnnotation(true),
		person,
	), listPersonTeamsHandler)

	s.AddTool(mcp.NewTool(toolAddTeamMembers,
		mcp.WithDescription("Add people to a team, all or none. Fails when a person is already a member or can't be a team member."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		team,
		mcp.WithArray("people", mcp.Required(), mcp.WithStringItems(), mcp.Description("Emails or UUIDs of the people to add")),
	), addTeamMembersHandler)

	s.AddTool(mcp.NewTool(toolRemoveTeamMember,
		mcp.WithDescription("Remove a person from a team. Only members added through the UI, API or MCP can be removed; "+
			"memberships a team mapper or an identity provider added are left to them."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		team, person,
	), removeTeamMemberHandler)
}
