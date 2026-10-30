package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/flanksource/commons/hash"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/gomplate/v3"
	"github.com/google/uuid"
	"github.com/patrickmn/go-cache"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/trace"

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
    (has(claims.org_role) && claims.org_role == "org:admin") ||
    (has(claims.o) && has(claims.o.rol) && claims.o.rol == "admin") ? ["admins"] :
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

	// teamMapperLocks serialises the syncs of each person, so concurrent requests with different input
	// can't each keep the teams they added.
	teamMapperLocks sync.Map
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
	lock, _ := teamMapperLocks.LoadOrStore(cacheKey, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	if last, ok := teamMapperInputs.Get(cacheKey); ok && last == inputHash {
		return nil
	}

	teams, err := runTeamMapper(ctx, expr, env)
	if err != nil {
		// The same input gives the same result, so it isn't retried until the input changes
		teamMapperInputs.SetDefault(cacheKey, inputHash)
		return err
	}

	if err := syncMappedTeams(ctx, personID, SourceTeamMapper, teams); err != nil {
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

	// A mapper returns a list or a map, or older scripts a JSON encoded string
	res, ok := out.(string)
	if !ok {
		b, err := json.Marshal(out)
		if err != nil {
			return nil, ctx.Oops().Wrapf(err, "team mapper returned %v", out)
		}
		res = string(b)
	}

	if strings.HasPrefix(strings.TrimSpace(res), "[") {
		var teams []string
		if err := json.Unmarshal([]byte(res), &teams); err != nil {
			return nil, ctx.Oops().With("result", res).Errorf("team mapper must return a list of team names, got %s", res)
		}
		return lo.Uniq(lo.Compact(teams)), nil
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
			Errorf("team mapper must return a list of teams or {\"teams\": [...]}, got %s", res)
	}

	return lo.Uniq(lo.Compact(*result.Teams)), nil
}

// syncMappedTeams makes the person's team memberships with the source exactly the given teams.
// Memberships with any other source, e.g. added by an admin, are never touched.
// A team that doesn't exist is skipped.
//
// The memberships are written in one transaction, and the policy is updated only once it commits,
// so a failed write changes nothing.
func syncMappedTeams(ctx context.Context, personID uuid.UUID, source string, teamNames []string) error {
	var added, removed []uuid.UUID
	err := ctx.Transaction(func(ctx context.Context, _ trace.Span) error {
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
		if err := ctx.DB().Where("person_id = ? AND source = ?", personID, source).Find(&mapped).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to get mapped teams of person %s", personID)
		}

		for _, team := range teams {
			if slices.ContainsFunc(mapped, func(m dbModels.TeamMember) bool { return m.TeamID == team.ID }) {
				continue
			}

			// A direct membership in the team is kept as it is
			result := ctx.DB().Exec(`INSERT INTO team_members (team_id, person_id, source) VALUES (?, ?, ?)
				ON CONFLICT (team_id, person_id) DO NOTHING`, team.ID, personID, source)
			if result.Error != nil {
				return ctx.Oops().Wrapf(result.Error, "failed to add person %s to team %s", personID, team.Name)
			} else if result.RowsAffected > 0 {
				added = append(added, team.ID)
			}
		}

		for _, m := range mapped {
			if slices.ContainsFunc(teams, func(t models.Team) bool { return t.ID == m.TeamID }) {
				continue
			}

			result := ctx.DB().Where("team_id = ? AND person_id = ? AND source = ?", m.TeamID, personID, source).
				Delete(&dbModels.TeamMember{})
			if result.Error != nil {
				return ctx.Oops().Wrapf(result.Error, "failed to remove person %s from team %s", personID, m.TeamID)
			} else if result.RowsAffected > 0 {
				// None removed means an admin made it a direct membership in the meantime
				removed = append(removed, m.TeamID)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	for _, teamID := range added {
		ctx.Logger.V(3).Infof("team mapper added person %s to team %s", personID, teamID)
		if err := applyMembership(personID, teamID, true); err != nil {
			return ctx.Oops().Wrapf(err, "failed to add person %s to team %s", personID, teamID)
		}
	}
	for _, teamID := range removed {
		ctx.Logger.V(3).Infof("team mapper removed person %s from team %s", personID, teamID)
		if err := applyMembership(personID, teamID, false); err != nil {
			return ctx.Oops().Wrapf(err, "failed to remove person %s from team %s", personID, teamID)
		}
	}

	return nil
}

// applyMembership updates the policy right away, so the request that triggered the sync already sees it.
// The team_members notification does the same shortly after.
func applyMembership(personID, teamID uuid.UUID, member bool) error {
	enforcer := dutyRBAC.Enforcer()
	if enforcer == nil {
		return nil
	}

	var err error
	if member {
		err = dutyRBAC.AddRoleForUser(personID.String(), teamID.String())
	} else {
		err = dutyRBAC.DeleteRoleForUser(personID.String(), teamID.String())
	}

	// Cached decisions don't account for grouping changes
	if cacheErr := enforcer.InvalidateCache(); err == nil {
		err = cacheErr
	}
	InvalidateRLSCacheForUser(personID.String())
	return err
}
