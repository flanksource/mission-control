package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/flanksource/commons/hash"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/gomplate/v3"
	"github.com/google/uuid"
	"github.com/patrickmn/go-cache"
	"github.com/samber/lo"

	dbModels "github.com/flanksource/incident-commander/db/models"
)

// SourceTeamMapper is the source of the team memberships a team mapper adds.
// A team mapper only ever removes memberships with this source.
const SourceTeamMapper = "team-mapper"

// DefaultClerkTeamMapper is used when --clerk-team-mapper isn't set.
// It puts organisation admins in admins, guests in no team and everyone else in viewers.
const DefaultClerkTeamMapper = `{
  "teams":
    (has(claims.role) && claims.role == "admin") ||
    (has(claims.org_role) && claims.org_role == "org:admin") ? ["admins"] :
    (has(claims.org_role) && claims.org_role == "org:guest") ? [] :
    ["viewers"]
}`

var (
	// IdentityRoleMapper is the Kratos team mapper. Its input is the person's Kratos identity.
	IdentityRoleMapper string

	// ClerkTeamMapper is the Clerk team mapper. Its input is the claims of the verified Clerk session token.
	ClerkTeamMapper string

	// teamMapperInputs holds, per person, a hash of the input the team mapper last ran on,
	// so it only runs again when that input changes.
	teamMapperInputs = cache.New(24*time.Hour, time.Hour)
)

// clerkVolatileClaims change on every token refresh without saying anything about the person.
var clerkVolatileClaims = []string{"exp", "iat", "nbf", "jti"}

// LoadTeamMapper returns the expression, reading it from a file when it's prefixed with file://.
func LoadTeamMapper(expr string) (string, error) {
	path, ok := strings.CutPrefix(expr, "file://")
	if !ok {
		return expr, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read team mapper file(%s): %w", path, err)
	}
	return string(content), nil
}

// mapTeams runs the team mapper on env, unless it already ran on the same input for this person,
// and syncs the person's mapped teams to its result.
// A result that isn't a list of teams is an error and changes nothing.
func mapTeams(ctx context.Context, personID uuid.UUID, expr string, env map[string]any, input any) error {
	inputHash, err := hashTeamMapperInput(input)
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to hash team mapper input")
	}

	cacheKey := personID.String()
	if last, ok := teamMapperInputs.Get(cacheKey); ok && last == inputHash {
		return nil
	}

	teams, err := runTeamMapper(ctx, expr, env)
	if err != nil {
		// The same input gives the same result, so it isn't retried until the input changes
		teamMapperInputs.SetDefault(cacheKey, inputHash)
		return err
	}

	if err := syncMappedTeams(ctx, personID, teams); err != nil {
		return err
	}

	teamMapperInputs.SetDefault(cacheKey, inputHash)
	return nil
}

func hashTeamMapperInput(input any) (string, error) {
	b, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	return hash.Sha256Hex(string(b)), nil
}

// runTeamMapper evaluates the team mapper and returns the team names it returns.
func runTeamMapper(ctx context.Context, expr string, env map[string]any) ([]string, error) {
	tmpl := gomplate.Template{Expression: expr}
	for _, f := range context.CelEnvFuncs {
		tmpl.CelEnvs = append(tmpl.CelEnvs, f(ctx))
	}

	out, err := gomplate.RunExpressionContext(ctx.Context, env, tmpl)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to run team mapper")
	}

	// A mapper returns a map, or older scripts a JSON encoded string
	res, ok := out.(string)
	if !ok {
		b, err := json.Marshal(out)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "team mapper returned %v", out)
		}
		res = string(b)
	}

	var result struct {
		Teams *[]string `json:"teams"`
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(res)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || result.Teams == nil {
		return nil, ctx.Oops().
			With("result", res).
			Hint("https://docs.flanksource.com/reference/helm/mission-control/#identity-mapper").
			Errorf("team mapper must return only {\"teams\": [...]}, got %s", res)
	}

	return lo.Uniq(lo.Compact(*result.Teams)), nil
}

// syncMappedTeams makes the person's mapped team memberships exactly the given teams.
// Memberships with any other source, e.g. added by an admin, are never touched.
// A team that doesn't exist is skipped.
func syncMappedTeams(ctx context.Context, personID uuid.UUID, teamNames []string) error {
	var teams []models.Team
	if len(teamNames) > 0 {
		if err := ctx.DB().Where("name IN ? AND deleted_at IS NULL", teamNames).Find(&teams).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to find teams %v", teamNames)
		}
	}

	for _, name := range teamNames {
		if !slices.ContainsFunc(teams, func(t models.Team) bool { return t.Name == name }) {
			ctx.Logger.Errorf("team mapper returned team %q for person %s, which doesn't exist", name, personID)
		}
	}

	var mapped []dbModels.TeamMember
	if err := ctx.DB().Where("person_id = ? AND source = ?", personID, SourceTeamMapper).Find(&mapped).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to get mapped teams of person %s", personID)
	}

	for _, team := range teams {
		if slices.ContainsFunc(mapped, func(m dbModels.TeamMember) bool { return m.TeamID == team.ID }) {
			continue
		}

		// A direct membership in the team is kept as it is
		result := ctx.DB().Exec(`INSERT INTO team_members (team_id, person_id, source) VALUES (?, ?, ?)
			ON CONFLICT (team_id, person_id) DO NOTHING`, team.ID, personID, SourceTeamMapper)
		if result.Error != nil {
			return ctx.Oops().Wrapf(result.Error, "failed to add person %s to team %s", personID, team.Name)
		}
		if result.RowsAffected > 0 {
			ctx.Logger.V(3).Infof("team mapper added person %s to team %s", personID, team.Name)
			if err := applyMembership(personID, team.ID, true); err != nil {
				return ctx.Oops().Wrapf(err, "failed to add person %s to team %s", personID, team.Name)
			}
		}
	}

	for _, m := range mapped {
		if slices.ContainsFunc(teams, func(t models.Team) bool { return t.ID == m.TeamID }) {
			continue
		}

		if err := ctx.DB().Where("team_id = ? AND person_id = ? AND source = ?", m.TeamID, personID, SourceTeamMapper).
			Delete(&dbModels.TeamMember{}).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to remove person %s from team %s", personID, m.TeamID)
		}
		ctx.Logger.V(3).Infof("team mapper removed person %s from team %s", personID, m.TeamID)
		if err := applyMembership(personID, m.TeamID, false); err != nil {
			return ctx.Oops().Wrapf(err, "failed to remove person %s from team %s", personID, m.TeamID)
		}
	}

	return nil
}

// applyMembership updates the policy right away, so the request that triggered the sync already sees it.
// The team_members notification does the same shortly after.
func applyMembership(personID, teamID uuid.UUID, member bool) error {
	if dutyRBAC.Enforcer() == nil {
		return nil
	}

	InvalidateRLSCacheForUser(personID.String())
	if member {
		return dutyRBAC.AddRoleForUser(personID.String(), teamID.String())
	}
	return dutyRBAC.DeleteRoleForUser(personID.String(), teamID.String())
}
