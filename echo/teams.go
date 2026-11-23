package echo

import (
	"net/http"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	echov4 "github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

func init() {
	RegisterRoutes(RegisterTeamRoutes)
}

// RegisterTeamRoutes registers the routes that create, change and delete teams and change their members. Changing
// members takes update on teams, as /db/team_members, which is read-only, did.
func RegisterTeamRoutes(e *echov4.Echo) {
	update := rbac.Authorization(adapter.ObjectTeams, policy.ActionUpdate)
	e.POST("/teams", CreateTeam, rbac.Authorization(adapter.ObjectTeams, policy.ActionCreate))
	e.PATCH("/teams/:id", UpdateTeam, update)
	e.DELETE("/teams/:id", DeleteTeam, rbac.Authorization(adapter.ObjectTeams, policy.ActionDelete))
	e.POST("/teams/:id/members", AddTeamMembers, update)
	e.DELETE("/teams/:id/members/:personId", RemoveTeamMember, update)
}

type CreateTeamRequest struct {
	Name string `json:"name"`
	Icon string `json:"icon"`
}

// CreateTeam creates a team without members.
func CreateTeam(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)

	var req CreateTeamRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request body: %v", err))
	}

	team, err := db.CreateTeam(ctx, req.Name, req.Icon)
	if err != nil {
		return api.WriteError(c, err)
	}
	return c.JSON(http.StatusCreated, team)
}

// UpdateTeam changes a team's name or icon.
func UpdateTeam(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)

	teamID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid team id %q", c.Param("id")))
	}

	var changes db.TeamChanges
	if err := c.Bind(&changes); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request body: %v", err))
	}

	team, err := db.UpdateTeam(ctx, teamID, changes)
	if err != nil {
		return api.WriteError(c, err)
	}
	return c.JSON(http.StatusOK, team)
}

// DeleteTeam deletes a team and removes its members.
func DeleteTeam(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)

	teamID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid team id %q", c.Param("id")))
	}

	if err := db.DeleteTeamWithMembers(ctx, teamID); err != nil {
		return api.WriteError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

type AddTeamMembersRequest struct {
	PersonIDs []uuid.UUID `json:"personIds"`
}

// AddTeamMembers adds people to the team, all or none.
func AddTeamMembers(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)

	teamID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid team id %q", c.Param("id")))
	}

	var req AddTeamMembersRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request body: %v", err))
	}
	if len(req.PersonIDs) == 0 {
		return api.WriteError(c, api.Errorf(api.EINVALID, "personIds is required"))
	}

	if err := db.AddTeamMembers(ctx, teamID, req.PersonIDs); err != nil {
		return api.WriteError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// RemoveTeamMember removes a direct member of the team.
func RemoveTeamMember(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)

	teamID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid team id %q", c.Param("id")))
	}
	personID, err := uuid.Parse(c.Param("personId"))
	if err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid person id %q", c.Param("personId")))
	}

	if err := db.RemoveTeamMember(ctx, teamID, personID); err != nil {
		return api.WriteError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
