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

// AccessGrant is one row of the access crosstab: a single (config, principal,
// role) grant. GroupName is resolved client-side from ExternalGroupID because
// config_access_summary carries only the group id (db/access_query.go joins
// external_groups server-side for the same value).
type AccessGrant struct {
	models.ConfigAccessSummary
	GroupName string `json:"group_name,omitempty"`
}

// RoleSource mirrors db.RBACAccessRow.RoleSource — "direct" for a grant held by
// the principal itself, "group:<name>" when it is inherited from a group.
func (g AccessGrant) RoleSource() string {
	if g.GroupName != "" {
		return "group:" + g.GroupName
	}
	return "direct"
}

type AccessGrantOptions struct {
	ConfigIDs  []string
	UserIDs    []string
	GroupIDs   []string
	ConfigType string
	User       string
	Role       string
	UserType   string
	Limit      int
}

func (o AccessGrantOptions) params() url.Values {
	params := url.Values{}
	params.Set("deleted_at", "is.null")
	params.Set("order", "config_name,user")
	if len(o.ConfigIDs) > 0 {
		params.Set("config_id", inList(o.ConfigIDs))
	}
	if len(o.UserIDs) > 0 {
		params.Set("external_user_id", inList(o.UserIDs))
	}
	if len(o.GroupIDs) > 0 {
		params.Set("external_group_id", inList(o.GroupIDs))
	}
	if o.ConfigType != "" {
		params.Set("config_type.filter", o.ConfigType)
	}
	if o.Role != "" {
		params.Set("role.filter", o.Role)
	}
	if o.UserType != "" {
		params.Set("user_type.filter", o.UserType)
	}
	if o.User != "" {
		params.Set("or", fmt.Sprintf(`(user.ilike.*%s*,email.ilike.*%s*)`, o.User, o.User))
	}
	if o.Limit > 0 {
		params.Set("limit", strconv.Itoa(o.Limit))
	}
	return params
}

// ListAccessGrants returns the flat grant rows behind the access crosstab,
// along with the server's exact total so callers can report truncation.
func (c *Client) ListAccessGrants(ctx context.Context, opts AccessGrantOptions) ([]AccessGrant, int, error) {
	var summaries []models.ConfigAccessSummary
	total, err := c.pgGet(ctx, "config_access_summary", opts.params(), &summaries)
	if err != nil {
		return nil, 0, err
	}

	groupIDs := make([]string, 0)
	seen := map[uuid.UUID]struct{}{}
	for _, s := range summaries {
		if s.ExternalGroupID == nil {
			continue
		}
		if _, ok := seen[*s.ExternalGroupID]; ok {
			continue
		}
		seen[*s.ExternalGroupID] = struct{}{}
		groupIDs = append(groupIDs, s.ExternalGroupID.String())
	}

	names, err := c.groupNames(ctx, groupIDs)
	if err != nil {
		return nil, 0, err
	}

	grants := make([]AccessGrant, 0, len(summaries))
	for _, s := range summaries {
		grant := AccessGrant{ConfigAccessSummary: s}
		if s.ExternalGroupID != nil {
			grant.GroupName = names[*s.ExternalGroupID]
		}
		grants = append(grants, grant)
	}
	return grants, total, nil
}

// ExpandGroupAccess emits every input grant followed by one synthetic grant per
// currently-active member of the granting group, so a group-held role shows the
// humans it actually reaches. Direct grants pass through unchanged.
//
// This is a client-side port of report/catalog.ExpandGroupAccess, which is typed
// on db.RBACAccessRow and cannot be imported without pulling the server DB layer
// into the slim client. It keys on group id rather than group name, so groups
// sharing a name across tenants no longer cross-contaminate.
func ExpandGroupAccess(grants []AccessGrant, members []GroupMember) []AccessGrant {
	if len(grants) == 0 {
		return grants
	}

	byGroup := make(map[uuid.UUID][]GroupMember)
	for _, m := range members {
		if m.MembershipDeletedAt != nil {
			continue
		}
		byGroup[m.GroupID] = append(byGroup[m.GroupID], m)
	}

	out := make([]AccessGrant, 0, len(grants))
	for _, grant := range grants {
		out = append(out, grant)
		if grant.ExternalGroupID == nil {
			continue
		}
		for _, m := range byGroup[*grant.ExternalGroupID] {
			synthetic := grant
			synthetic.ExternalUserID = m.UserID
			synthetic.User = m.UserName
			synthetic.Email = m.Email
			synthetic.UserType = m.UserType
			synthetic.LastSignedInAt = m.LastSignedInAt
			synthetic.LastReviewedAt = nil
			synthetic.CreatedAt = m.MembershipAddedAt
			out = append(out, synthetic)
		}
	}
	return out
}

// AccessSummaryByUser is a row of config_access_summary_by_user: one principal
// with its access rollup.
type AccessSummaryByUser struct {
	ExternalUserID  uuid.UUID  `json:"external_user_id"`
	User            string     `json:"user"`
	Email           string     `json:"email"`
	AccessCount     int        `json:"access_count"`
	DistinctRoles   int        `json:"distinct_roles"`
	DistinctConfigs int        `json:"distinct_configs"`
	LastSignedInAt  *time.Time `json:"last_signed_in_at,omitempty"`
	LatestGrant     *time.Time `json:"latest_grant,omitempty"`
}

// AccessSummaryByConfig is a row of config_access_summary_by_config: one config
// with its access rollup.
type AccessSummaryByConfig struct {
	ConfigID       uuid.UUID  `json:"config_id"`
	ConfigName     string     `json:"config_name"`
	ConfigType     string     `json:"config_type"`
	AccessCount    int        `json:"access_count"`
	DistinctUsers  int        `json:"distinct_users"`
	DistinctRoles  int        `json:"distinct_roles"`
	LastSignedInAt *time.Time `json:"last_signed_in_at,omitempty"`
	LatestGrant    *time.Time `json:"latest_grant,omitempty"`
}

// CanUseConfigRollup reports whether config_access_summary_by_config can answer
// these options directly. The view aggregates away every principal column, so
// any principal-level filter forces a client-side rollup of the grant rows.
func (o AccessGrantOptions) CanUseConfigRollup() bool {
	return o.User == "" && o.Role == "" && o.UserType == "" && len(o.UserIDs) == 0 && len(o.GroupIDs) == 0
}

// CanUseUserRollup reports whether config_access_summary_by_user can answer
// these options directly. That view aggregates away the config columns too, so
// it additionally requires no config filter.
func (o AccessGrantOptions) CanUseUserRollup() bool {
	return o.CanUseConfigRollup() && len(o.ConfigIDs) == 0 && o.ConfigType == ""
}

// ListAccessSummaryByUser returns the server-computed per-user rollup, which
// covers every grant in the database. Only valid when CanUseUserRollup.
func (c *Client) ListAccessSummaryByUser(ctx context.Context, limit int) ([]AccessSummaryByUser, int, error) {
	params := url.Values{}
	params.Set("order", "access_count.desc,user")
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	var out []AccessSummaryByUser
	total, err := c.pgGet(ctx, "config_access_summary_by_user", params, &out)
	return out, total, err
}

// ListAccessSummaryByConfig returns the server-computed per-config rollup,
// narrowed by the config filters. Only valid when CanUseConfigRollup.
func (c *Client) ListAccessSummaryByConfig(ctx context.Context, opts AccessGrantOptions) ([]AccessSummaryByConfig, int, error) {
	params := url.Values{}
	params.Set("order", "access_count.desc,config_name")
	if len(opts.ConfigIDs) > 0 {
		params.Set("config_id", inList(opts.ConfigIDs))
	}
	if opts.ConfigType != "" {
		params.Set("config_type.filter", opts.ConfigType)
	}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	var out []AccessSummaryByConfig
	total, err := c.pgGet(ctx, "config_access_summary_by_config", params, &out)
	return out, total, err
}

// groupNames maps external group ids to their names.
func (c *Client) groupNames(ctx context.Context, ids []string) (map[uuid.UUID]string, error) {
	names := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return names, nil
	}

	params := url.Values{}
	params.Set("select", "id,name")

	groups, err := pgGetIn[struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	}](ctx, c, "external_groups", "id", ids, params)
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		names[g.ID] = g.Name
	}
	return names, nil
}

// ConfigRef is the minimal config identity used to hydrate names onto rows that
// carry only a config id.
type ConfigRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Type string    `json:"type"`
}

// ConfigRefs resolves config ids to their name and type. Unlike GetCatalogItems
// this pulls three columns rather than whole config bodies.
func (c *Client) ConfigRefs(ctx context.Context, ids []string) (map[uuid.UUID]ConfigRef, error) {
	refs := map[uuid.UUID]ConfigRef{}
	if len(ids) == 0 {
		return refs, nil
	}

	params := url.Values{}
	params.Set("select", "id,name,type")

	items, err := pgGetIn[ConfigRef](ctx, c, "config_items", "id", ids, params)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		refs[item.ID] = item
	}
	return refs, nil
}

// pgGet issues a PostgREST GET against /db/<table> and decodes the rows. The
// returned total is the exact row count reported via Content-Range, or -1 when
// the server did not compute one. count=exact is requested only for limited
// queries — that is the only case where the caller needs to know it truncated,
// and an exact count is a second full scan.
func (c *Client) pgGet(ctx context.Context, table string, params url.Values, out any) (int, error) {
	req := c.R(ctx)
	for key, values := range params {
		for _, value := range values {
			req = req.QueryParam(key, value)
		}
	}
	if params.Get("limit") != "" {
		req = req.Header("Prefer", "count=exact")
	}

	r, err := req.Get(c.apiPath("/db/" + table))
	if err != nil {
		return 0, err
	}
	if !r.IsOK() {
		body, _ := r.AsString()
		if looksLikeHTML(r.Header.Get("Content-Type"), body) {
			return 0, ErrHTMLResponse
		}
		return 0, fmt.Errorf("server returned %d: %s", r.StatusCode, strings.TrimSpace(body))
	}
	if err := decodeJSON(r, out); err != nil {
		return 0, err
	}
	return parseContentRangeTotal(r.Header.Get("Content-Range")), nil
}

// parseContentRangeTotal extracts the total from a PostgREST Content-Range
// header ("0-24/3573"). Returns -1 when the total is unknown ("0-24/*").
func parseContentRangeTotal(header string) int {
	_, totalPart, ok := strings.Cut(header, "/")
	if !ok {
		return -1
	}
	total, err := strconv.Atoi(strings.TrimSpace(totalPart))
	if err != nil {
		return -1
	}
	return total
}

// inList builds a PostgREST `in.(a,b,c)` filter.
func inList(values []string) string {
	return "in.(" + strings.Join(uniqueIDs(values), ",") + ")"
}

// uniqueIDs drops blanks and duplicates and sorts, so the same set always
// produces the same URL and no row is fetched twice across batches.
func uniqueIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		unique = append(unique, v)
	}
	sort.Strings(unique)
	return unique
}
