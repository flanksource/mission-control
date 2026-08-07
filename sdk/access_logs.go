package sdk

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

// accessLogSelect embeds the acting user, the same projection the web UI reads
// (flanksource-ui/src/api/services/configAccess.ts).
const accessLogSelect = "*,external_users(name,user_email:email)"

// AccessLogUser is the embedded user identity on an access log row.
type AccessLogUser struct {
	Name  string  `json:"name"`
	Email *string `json:"user_email"`
}

// AccessLog is one sign-in record against a config, with the acting user and
// config identity hydrated.
type AccessLog struct {
	models.ConfigAccessLog
	User       *AccessLogUser `json:"external_users,omitempty"`
	ConfigName string         `json:"config_name,omitempty"`
	ConfigType string         `json:"config_type,omitempty"`
}

// UserName returns the acting user's name, or the raw user id when the user
// record is no longer resolvable.
func (l AccessLog) UserName() string {
	if l.User != nil && l.User.Name != "" {
		return l.User.Name
	}
	return l.ExternalUserID.String()
}

// UserEmail returns the acting user's email, if known.
func (l AccessLog) UserEmail() string {
	if l.User != nil && l.User.Email != nil {
		return *l.User.Email
	}
	return ""
}

// AccessReview is one recorded review of a grant, with config, principal and
// role names hydrated.
type AccessReview struct {
	models.AccessReview
	ConfigName string `json:"config_name,omitempty"`
	ConfigType string `json:"config_type,omitempty"`
	User       string `json:"user,omitempty"`
	Role       string `json:"role,omitempty"`
}

// AccessHistoryOptions filters the time-ordered access surfaces.
type AccessHistoryOptions struct {
	ConfigIDs []string
	UserIDs   []string
	Since     *time.Time
	Limit     int
}

func (o AccessHistoryOptions) params(userColumn string) url.Values {
	params := url.Values{}
	params.Set("order", "created_at.desc")
	if len(o.ConfigIDs) > 0 {
		params.Set("config_id", inList(o.ConfigIDs))
	}
	if len(o.UserIDs) > 0 {
		params.Set(userColumn, inList(o.UserIDs))
	}
	if o.Since != nil {
		params.Set("created_at", "gte."+o.Since.UTC().Format(time.RFC3339))
	}
	if o.Limit > 0 {
		params.Set("limit", strconv.Itoa(o.Limit))
	}
	return params
}

// ListAccessLogs returns sign-in records against configs, newest first.
func (c *Client) ListAccessLogs(ctx context.Context, opts AccessHistoryOptions) ([]AccessLog, int, error) {
	params := opts.params("external_user_id")
	params.Set("select", accessLogSelect)

	var logs []AccessLog
	total, err := c.pgGet(ctx, "config_access_logs", params, &logs)
	if err != nil {
		return nil, 0, err
	}

	configIDs := make([]string, 0, len(logs))
	for _, l := range logs {
		configIDs = append(configIDs, l.ConfigID.String())
	}
	refs, err := c.ConfigRefs(ctx, configIDs)
	if err != nil {
		return nil, 0, err
	}
	for i := range logs {
		if ref, ok := refs[logs[i].ConfigID]; ok {
			logs[i].ConfigName = ref.Name
			logs[i].ConfigType = ref.Type
		}
	}
	return logs, total, nil
}

// ListAccessReviews returns recorded grant reviews, newest first.
func (c *Client) ListAccessReviews(ctx context.Context, opts AccessHistoryOptions) ([]AccessReview, int, error) {
	var reviews []AccessReview
	total, err := c.pgGet(ctx, "access_reviews", opts.params("external_user_id"), &reviews)
	if err != nil {
		return nil, 0, err
	}
	if err := c.hydrateReviews(ctx, reviews); err != nil {
		return nil, 0, err
	}
	return reviews, total, nil
}

// hydrateReviews fills in config, principal and role names, which access_reviews
// stores only as ids.
func (c *Client) hydrateReviews(ctx context.Context, reviews []AccessReview) error {
	var configIDs, userIDs, groupIDs, roleIDs []string
	for _, r := range reviews {
		configIDs = append(configIDs, r.ConfigID.String())
		roleIDs = append(roleIDs, r.ExternalRoleID.String())
		if r.ExternalUserID != nil {
			userIDs = append(userIDs, r.ExternalUserID.String())
		}
		if r.ExternalGroupID != nil {
			groupIDs = append(groupIDs, r.ExternalGroupID.String())
		}
	}

	refs, err := c.ConfigRefs(ctx, configIDs)
	if err != nil {
		return err
	}

	nameSelect := url.Values{}
	nameSelect.Set("select", "id,name")

	users, err := pgGetIn[namedRow](ctx, c, "external_users", "id", userIDs, nameSelect)
	if err != nil {
		return err
	}
	groups, err := pgGetIn[namedRow](ctx, c, "external_groups", "id", groupIDs, nameSelect)
	if err != nil {
		return err
	}
	roles, err := pgGetIn[namedRow](ctx, c, "external_roles", "id", roleIDs, nameSelect)
	if err != nil {
		return err
	}

	principals := indexByID(users)
	for id, name := range indexByID(groups) {
		principals[id] = name
	}
	roleNames := indexByID(roles)

	for i := range reviews {
		if ref, ok := refs[reviews[i].ConfigID]; ok {
			reviews[i].ConfigName = ref.Name
			reviews[i].ConfigType = ref.Type
		}
		reviews[i].Role = roleNames[reviews[i].ExternalRoleID]
		if reviews[i].ExternalUserID != nil {
			reviews[i].User = principals[*reviews[i].ExternalUserID]
		} else if reviews[i].ExternalGroupID != nil {
			reviews[i].User = principals[*reviews[i].ExternalGroupID]
		}
	}
	return nil
}

type namedRow struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

func indexByID(rows []namedRow) map[uuid.UUID]string {
	out := make(map[uuid.UUID]string, len(rows))
	for _, row := range rows {
		out[row.ID] = row.Name
	}
	return out
}
