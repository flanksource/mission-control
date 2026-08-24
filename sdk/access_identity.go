package sdk

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

// accessIDBatchSize bounds how many ids go into a single `in.(...)` filter so a
// wide expansion does not overflow the request URL.
const accessIDBatchSize = 100

// ExternalGroupSummary is a row of the external_group_summary view: an external
// group plus its member and permission counts.
type ExternalGroupSummary struct {
	models.ExternalGroup
	MembersCount     int `json:"members_count"`
	PermissionsCount int `json:"permissions_count"`
}

// GroupMember is one user's membership of one group, including soft-deleted
// memberships so an audit can see revoked access.
type GroupMember struct {
	GroupID             uuid.UUID  `json:"external_group_id"`
	GroupName           string     `json:"group_name"`
	GroupType           string     `json:"group_type"`
	UserID              uuid.UUID  `json:"external_user_id"`
	UserName            string     `json:"user_name"`
	Email               string     `json:"email"`
	UserType            string     `json:"user_type"`
	LastSignedInAt      *time.Time `json:"last_signed_in_at,omitempty"`
	MembershipAddedAt   time.Time  `json:"membership_created_at"`
	MembershipDeletedAt *time.Time `json:"membership_deleted_at,omitempty"`
}

// Active reports whether the membership is still in force.
func (m GroupMember) Active() bool { return m.MembershipDeletedAt == nil }

// IdentityOptions filters the external identity listings.
type IdentityOptions struct {
	Name  string
	Type  string
	Limit int
}

func (o IdentityOptions) params(typeColumn string) url.Values {
	params := url.Values{}
	params.Set("deleted_at", "is.null")
	params.Set("order", "name")
	if o.Type != "" {
		params.Set(typeColumn+".filter", o.Type)
	}
	if o.Limit > 0 {
		params.Set("limit", strconv.Itoa(o.Limit))
	}
	return params
}

// ListExternalUsers applies MatchItem patterns across id, name, email and aliases
// before limiting the active users returned by the server.
func (c *Client) ListExternalUsers(ctx context.Context, opts IdentityOptions) ([]models.ExternalUser, int, error) {
	params := opts.params("user_type")
	if opts.Name == "" {
		var out []models.ExternalUser
		total, err := c.pgGet(ctx, "external_users", params, &out)
		return out, total, err
	}
	ids, total, err := c.matchingIdentityIDs(ctx, opts, userIdentityMatch)
	if err != nil || len(ids) == 0 {
		return nil, total, err
	}
	out, err := pgGetIn[models.ExternalUser](ctx, c, "external_users", "id", ids, params)
	return out, total, err
}

// ListExternalGroups reads external_group_summary so each group arrives with its
// member and permission counts already rolled up.
func (c *Client) ListExternalGroups(ctx context.Context, opts IdentityOptions) ([]ExternalGroupSummary, int, error) {
	params := opts.params("group_type")
	if opts.Name == "" {
		var out []ExternalGroupSummary
		total, err := c.pgGet(ctx, "external_group_summary", params, &out)
		return out, total, err
	}
	ids, total, err := c.matchingIdentityIDs(ctx, opts, groupIdentityMatch)
	if err != nil || len(ids) == 0 {
		return nil, total, err
	}
	out, err := pgGetIn[ExternalGroupSummary](ctx, c, "external_group_summary", "id", ids, params)
	return out, total, err
}

// ListExternalRoles applies MatchItem patterns across role id and name.
func (c *Client) ListExternalRoles(ctx context.Context, opts IdentityOptions) ([]models.ExternalRole, int, error) {
	params := opts.params("role_type")
	if opts.Name == "" {
		var out []models.ExternalRole
		total, err := c.pgGet(ctx, "external_roles", params, &out)
		return out, total, err
	}
	ids, total, err := c.matchingIdentityIDs(ctx, opts, roleIdentityMatch)
	if err != nil || len(ids) == 0 {
		return nil, total, err
	}
	out, err := pgGetIn[models.ExternalRole](ctx, c, "external_roles", "id", ids, params)
	return out, total, err
}

// GetGroupMembers resolves the membership of the given groups, hydrating user
// identity and last sign-in. Soft-deleted memberships are returned too, flagged
// via MembershipDeletedAt, matching db.GetGroupMembers.
//
// Last sign-in comes from config_access_summary_by_user rather than a GROUP BY
// over config_access_logs (PostgREST cannot aggregate), so it reflects sign-ins
// on configs the user still holds a grant for.
func (c *Client) GetGroupMembers(ctx context.Context, groupIDs []string) ([]GroupMember, error) {
	if len(groupIDs) == 0 {
		return nil, nil
	}

	membershipParams := url.Values{}
	membershipParams.Set("select", "external_user_id,external_group_id,created_at,deleted_at")
	memberships, err := pgGetIn[struct {
		UserID    uuid.UUID  `json:"external_user_id"`
		GroupID   uuid.UUID  `json:"external_group_id"`
		CreatedAt time.Time  `json:"created_at"`
		DeletedAt *time.Time `json:"deleted_at,omitempty"`
	}](ctx, c, "external_user_groups", "external_group_id", groupIDs, membershipParams)
	if err != nil {
		return nil, err
	}
	if len(memberships) == 0 {
		return nil, nil
	}

	userIDs := make([]string, 0, len(memberships))
	for _, m := range memberships {
		userIDs = append(userIDs, m.UserID.String())
	}

	userParams := url.Values{}
	userParams.Set("select", "id,name,email,user_type")
	users, err := pgGetIn[struct {
		ID       uuid.UUID `json:"id"`
		Name     string    `json:"name"`
		Email    *string   `json:"email"`
		UserType string    `json:"user_type"`
	}](ctx, c, "external_users", "id", userIDs, userParams)
	if err != nil {
		return nil, err
	}
	usersByID := make(map[uuid.UUID]int, len(users))
	for i, u := range users {
		usersByID[u.ID] = i
	}

	signInParams := url.Values{}
	signInParams.Set("select", "external_user_id,last_signed_in_at")
	signIns, err := pgGetIn[struct {
		UserID         uuid.UUID  `json:"external_user_id"`
		LastSignedInAt *time.Time `json:"last_signed_in_at"`
	}](ctx, c, "config_access_summary_by_user", "external_user_id", userIDs, signInParams)
	if err != nil {
		return nil, err
	}
	lastSignIn := make(map[uuid.UUID]*time.Time, len(signIns))
	for _, s := range signIns {
		lastSignIn[s.UserID] = s.LastSignedInAt
	}

	groupParams := url.Values{}
	groupParams.Set("select", "id,name,group_type")
	groups, err := pgGetIn[struct {
		ID        uuid.UUID `json:"id"`
		Name      string    `json:"name"`
		GroupType string    `json:"group_type"`
	}](ctx, c, "external_groups", "id", groupIDs, groupParams)
	if err != nil {
		return nil, err
	}
	groupsByID := make(map[uuid.UUID]int, len(groups))
	for i, g := range groups {
		groupsByID[g.ID] = i
	}

	out := make([]GroupMember, 0, len(memberships))
	for _, m := range memberships {
		member := GroupMember{
			GroupID:             m.GroupID,
			UserID:              m.UserID,
			LastSignedInAt:      lastSignIn[m.UserID],
			MembershipAddedAt:   m.CreatedAt,
			MembershipDeletedAt: m.DeletedAt,
		}
		if i, ok := groupsByID[m.GroupID]; ok {
			member.GroupName = groups[i].Name
			member.GroupType = groups[i].GroupType
		}
		if i, ok := usersByID[m.UserID]; ok {
			member.UserName = users[i].Name
			member.UserType = users[i].UserType
			if users[i].Email != nil {
				member.Email = *users[i].Email
			}
		}
		out = append(out, member)
	}

	// Active memberships first, then by user name — same ordering as the server.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Active() != out[j].Active() {
			return out[i].Active()
		}
		return out[i].UserName < out[j].UserName
	})
	return out, nil
}

// GetGroupsForUser returns the groups a user is currently a member of.
func (c *Client) GetGroupsForUser(ctx context.Context, userID string) ([]models.ExternalGroup, error) {
	params := url.Values{}
	params.Set("external_user_id", "eq."+userID)
	params.Set("deleted_at", "is.null")
	params.Set("select", "external_group_id")

	var memberships []struct {
		GroupID uuid.UUID `json:"external_group_id"`
	}
	if _, err := c.pgGet(ctx, "external_user_groups", params, &memberships); err != nil {
		return nil, err
	}
	if len(memberships) == 0 {
		return nil, nil
	}

	ids := make([]string, 0, len(memberships))
	for _, m := range memberships {
		ids = append(ids, m.GroupID.String())
	}

	groupParams := url.Values{}
	groupParams.Set("deleted_at", "is.null")
	groupParams.Set("order", "name")
	groups, err := pgGetIn[models.ExternalGroup](ctx, c, "external_groups", "id", ids, groupParams)
	if err != nil {
		return nil, err
	}
	return groups, nil
}

// RoleHolders are the principals a role is granted to.
type RoleHolders struct {
	Users  []models.ExternalUser  `json:"users,omitempty"`
	Groups []models.ExternalGroup `json:"groups,omitempty"`
}

// GetRoleHolders resolves the users and groups holding an external role through
// live (non-deleted) config_access grants.
func (c *Client) GetRoleHolders(ctx context.Context, roleID string) (*RoleHolders, error) {
	params := url.Values{}
	params.Set("external_role_id", "eq."+roleID)
	params.Set("deleted_at", "is.null")
	params.Set("select", "external_user_id,external_group_id")

	var grants []struct {
		UserID  *uuid.UUID `json:"external_user_id"`
		GroupID *uuid.UUID `json:"external_group_id"`
	}
	if _, err := c.pgGet(ctx, "config_access", params, &grants); err != nil {
		return nil, err
	}

	var userIDs, groupIDs []string
	for _, g := range grants {
		if g.UserID != nil {
			userIDs = append(userIDs, g.UserID.String())
		}
		if g.GroupID != nil {
			groupIDs = append(groupIDs, g.GroupID.String())
		}
	}

	holders := &RoleHolders{}
	principalParams := url.Values{}
	principalParams.Set("deleted_at", "is.null")
	principalParams.Set("order", "name")

	var err error
	if holders.Users, err = pgGetIn[models.ExternalUser](ctx, c, "external_users", "id", userIDs, principalParams); err != nil {
		return nil, err
	}
	if holders.Groups, err = pgGetIn[models.ExternalGroup](ctx, c, "external_groups", "id", groupIDs, principalParams); err != nil {
		return nil, err
	}
	return holders, nil
}

// ResolveExternalUser resolves a UUID, name/email substring or alias to exactly
// one user, erroring on zero or multiple matches — same contract as the
// server-side resolveExternalUserArg.
func (c *Client) ResolveExternalUser(ctx context.Context, arg string) (*models.ExternalUser, error) {
	var users []models.ExternalUser
	if _, err := c.pgGet(ctx, "external_users", resolveParams(arg, nameOrEmailFilter(arg)), &users); err != nil {
		return nil, err
	}
	switch len(users) {
	case 0:
		return nil, fmt.Errorf("no external user matches %q", arg)
	case 1:
		return &users[0], nil
	default:
		return nil, fmt.Errorf("%q matches multiple users: %s", arg, namesOf(users, func(u models.ExternalUser) string { return u.Name }))
	}
}

// ResolveExternalGroup resolves a UUID, name substring or alias to one group.
func (c *Client) ResolveExternalGroup(ctx context.Context, arg string) (*models.ExternalGroup, error) {
	var groups []models.ExternalGroup
	if _, err := c.pgGet(ctx, "external_groups", resolveParams(arg, nameOrAliasFilter(arg)), &groups); err != nil {
		return nil, err
	}
	switch len(groups) {
	case 0:
		return nil, fmt.Errorf("no external group matches %q", arg)
	case 1:
		return &groups[0], nil
	default:
		return nil, fmt.Errorf("%q matches multiple groups: %s", arg, namesOf(groups, func(g models.ExternalGroup) string { return g.Name }))
	}
}

// ResolveExternalRole resolves a UUID or name substring to one role.
func (c *Client) ResolveExternalRole(ctx context.Context, arg string) (*models.ExternalRole, error) {
	params := resolveParams(arg, "")
	if params.Get("id") == "" {
		params.Set("name", "ilike.*"+arg+"*")
	}
	var roles []models.ExternalRole
	if _, err := c.pgGet(ctx, "external_roles", params, &roles); err != nil {
		return nil, err
	}
	switch len(roles) {
	case 0:
		return nil, fmt.Errorf("no external role matches %q", arg)
	case 1:
		return &roles[0], nil
	default:
		return nil, fmt.Errorf("%q matches multiple roles: %s", arg, namesOf(roles, func(r models.ExternalRole) string { return r.Name }))
	}
}

// resolveParams narrows to an exact id when arg is a UUID, otherwise applies the
// supplied `or` filter with a limit of 2 so ambiguity is detectable but cheap.
func resolveParams(arg, orFilter string) url.Values {
	params := url.Values{}
	params.Set("deleted_at", "is.null")
	if id, err := uuid.Parse(arg); err == nil {
		params.Set("id", "eq."+id.String())
		return params
	}
	if orFilter != "" {
		params.Set("or", orFilter)
	}
	params.Set("limit", "2")
	return params
}

func nameOrEmailFilter(value string) string {
	return fmt.Sprintf("(name.ilike.*%s*,email.ilike.*%s*,aliases.cs.{%s})", value, value, value)
}

func nameOrAliasFilter(value string) string {
	return fmt.Sprintf("(name.ilike.*%s*,aliases.cs.{%s})", value, value)
}

func namesOf[T any](items []T, name func(T) string) string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, name(item))
	}
	return strings.Join(names, ", ")
}

// pgGetIn fetches rows matching `column in (ids)` in bounded batches so long id
// lists do not overflow the request URL.
func pgGetIn[T any](ctx context.Context, c *Client, table, column string, ids []string, params url.Values) ([]T, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil, nil
	}

	var out []T
	for start := 0; start < len(ids); start += accessIDBatchSize {
		end := min(start+accessIDBatchSize, len(ids))

		batchParams := url.Values{}
		for key, values := range params {
			batchParams[key] = values
		}
		batchParams.Set(column, inList(ids[start:end]))

		var batch []T
		if _, err := c.pgGet(ctx, table, batchParams, &batch); err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}
