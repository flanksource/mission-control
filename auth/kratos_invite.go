package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

func (k *KratosHandler) createInvite(ctx context.Context, req InviteUserRequest) (*models.Invite, error) {
	email := normalizeEmail(req.Email)
	if email == "" {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "email is required")
	}

	if strings.TrimSpace(req.Role) != "" {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "invites can't give a role: built-in roles were removed, name teams instead")
	}

	grants := inviteGrants{
		Teams: lo.Uniq(lo.Compact(lo.Map(req.Teams, func(t string, _ int) string { return strings.TrimSpace(t) }))),
	}

	var existingID string
	if err := ctx.DB().Raw(`SELECT id FROM identities WHERE lower(traits->>'email') = ? LIMIT 1`, email).Scan(&existingID).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "error checking existing identity")
	}
	if existingID != "" {
		return nil, dutyAPI.Errorf(dutyAPI.ECONFLICT, "user already exists")
	}

	encoded, err := grants.encode()
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "error encoding invite")
	}

	invite := &models.Invite{Email: email, Role: encoded}
	if user := ctx.User(); user != nil {
		invite.InvitedBy = &user.ID
	}

	if err := ctx.DB().Create(invite).Error; err != nil {
		if existing, lookupErr := getKratosInvite(ctx, email); lookupErr == nil && existing.ID != invite.ID {
			return nil, dutyAPI.Errorf(dutyAPI.ECONFLICT, "pending invite already exists")
		}
		return nil, ctx.Oops().Wrapf(err, "error creating invite")
	}

	return invite, nil
}

func KratosErrorRedirect(c echo.Context) error {
	q := url.Values{}
	q.Set("error", "invite_required")
	if id := c.QueryParam("id"); id != "" {
		q.Set("kratos_error_id", id)
	}
	return c.Redirect(http.StatusFound, "/login?"+q.Encode())
}

func (k *KratosHandler) BeforeRegistrationWebhook(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var payload map[string]any
	if err := c.Bind(&payload); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}

	inviteID := extractKratosInviteID(payload)
	if inviteID == "" {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "registration is invite-only"))
	}

	if _, err := getPendingInviteByID(ctx, inviteID); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (k *KratosHandler) AfterRegistrationWebhook(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var payload map[string]any
	if err := c.Bind(&payload); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}

	identityID := extractIdentityID(payload)
	email := normalizeEmail(extractKratosWebhookEmail(payload))
	if email == "" {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "registration email is required"))
	}

	invite, err := getKratosInvite(ctx, email)
	if err != nil {
		if identityID != "" && hasAcceptedKratosInvite(ctx, email) {
			return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
		}
		return dutyAPI.WriteError(c, err)
	}

	if identityID != "" {
		// The webhook isn't authenticated, so the payload's identity is only trusted when Kratos stored it with the invite's email
		if err := verifyInvitedIdentity(ctx, identityID, email); err != nil {
			return dutyAPI.WriteError(c, err)
		}

		grants, err := decodeInviteGrants(invite.Role)
		if err != nil {
			return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "failed to read invite %s", invite.ID))
		}

		if err := joinInvitedTeams(ctx, identityID, grants.Teams); err != nil {
			return dutyAPI.WriteError(c, err)
		}
		if err := acceptKratosInvite(ctx, invite.ID.String()); err != nil {
			return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "failed to accept invite"))
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// verifyInvitedIdentity returns an error unless Kratos has stored the identity with the invited email.
func verifyInvitedIdentity(ctx context.Context, identityID, email string) error {
	if _, err := uuid.Parse(identityID); err != nil {
		return dutyAPI.Errorf(dutyAPI.EINVALID, "invalid identity id %q", identityID)
	}

	var stored string
	if err := ctx.DB().Raw(`SELECT traits->>'email' FROM identities WHERE id = ?`, identityID).Scan(&stored).Error; err != nil {
		return ctx.Oops().Wrapf(err, "error looking up identity %s", identityID)
	}
	if normalizeEmail(stored) != email {
		return dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "identity %s wasn't registered with the invited email", identityID)
	}
	return nil
}

// inviteGrants are what an invite gives the person when they accept it.
//
// duty's invites table has no column for teams yet, so they're stored as JSON in its role column.
// Move Teams to a column of its own once duty has one.
type inviteGrants struct {
	Teams []string `json:"teams,omitempty"`
}

func (g inviteGrants) encode() (string, error) {
	if len(g.Teams) == 0 {
		return "", nil
	}
	b, err := json.Marshal(g)
	return string(b), err
}

// decodeInviteGrants reads an invite's role column, written by inviteGrants.encode.
// An invite of an earlier version may hold a built-in role's name there, which grants nothing.
func decodeInviteGrants(column string) (inviteGrants, error) {
	if !strings.HasPrefix(column, "{") {
		return inviteGrants{}, nil
	}
	var g inviteGrants
	err := json.Unmarshal([]byte(column), &g)
	return g, err
}

// SourceInvite is the source of the team memberships a person gets by accepting an invite.
// Like memberships an admin adds, team mappers never touch them.
const SourceInvite = "invite"

// joinInvitedTeams adds the person to the teams their invite names. A team that doesn't exist is skipped and logged.
func joinInvitedTeams(ctx context.Context, identityID string, teamNames []string) error {
	if len(teamNames) == 0 {
		return nil
	}

	personID, err := uuid.Parse(identityID)
	if err != nil {
		return dutyAPI.Errorf(dutyAPI.EINVALID, "invalid identity id %q", identityID)
	}

	var teams []models.Team
	if err := ctx.DB().Where("name IN ? AND deleted_at IS NULL", teamNames).Find(&teams).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to find teams %v", teamNames)
	}

	for _, name := range teamNames {
		if !slices.ContainsFunc(teams, func(t models.Team) bool { return t.Name == name }) {
			ctx.Logger.Warnf("invite of person %s names team %q, which doesn't exist", personID, name)
		}
	}

	for _, team := range teams {
		result := ctx.DB().Exec(`INSERT INTO team_members (team_id, person_id, source) VALUES (?, ?, ?)
			ON CONFLICT (team_id, person_id) DO NOTHING`, team.ID, personID, SourceInvite)
		if result.Error != nil {
			return ctx.Oops().Wrapf(result.Error, "failed to add person %s to team %s", personID, team.Name)
		}
		if result.RowsAffected > 0 {
			if err := applyMembership(personID, team.ID, true); err != nil {
				return ctx.Oops().Wrapf(err, "failed to add person %s to team %s", personID, team.Name)
			}
		}
	}

	return nil
}

func getKratosInvite(ctx context.Context, email string) (*models.Invite, error) {
	var invite models.Invite
	err := ctx.DB().Where("lower(email) = ?", email).Where("accepted_at IS NULL").First(&invite).Error
	if err == nil {
		return &invite, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "registration is invite-only")
	}
	return nil, ctx.Oops().Wrapf(err, "error looking up invite")
}

func getPendingInviteByID(ctx context.Context, id string) (*models.Invite, error) {
	var invite models.Invite
	err := ctx.DB().Where("id = ?", id).Where("accepted_at IS NULL").First(&invite).Error
	if err == nil {
		return &invite, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "registration is invite-only")
	}
	return nil, ctx.Oops().Wrapf(err, "error looking up invite")
}

func acceptKratosInvite(ctx context.Context, inviteID string) error {
	return ctx.DB().Model(&models.Invite{}).
		Where("id = ?", inviteID).
		Where("accepted_at IS NULL").
		Update("accepted_at", gorm.Expr("NOW()")).Error
}

func hasAcceptedKratosInvite(ctx context.Context, email string) bool {
	var count int64
	ctx.DB().Model(&models.Invite{}).Where("lower(email) = ?", email).Where("accepted_at IS NOT NULL").Count(&count)
	return count > 0
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func registrationLink(inviteID string) string {
	frontendURL := strings.TrimRight(api.FrontendURL, "/")
	q := url.Values{}
	q.Set("invite", inviteID)
	return fmt.Sprintf("%s/login?%s", frontendURL, q.Encode())
}

func extractKratosInviteID(payload map[string]any) string {
	if transient, ok := payload["transient_payload"].(map[string]any); ok {
		if invite, ok := transient["invite"].(string); ok {
			return strings.TrimSpace(invite)
		}
	}

	for _, key := range []string{"request_url", "return_to"} {
		if invite := extractInviteFromURL(getString(payload, key)); invite != "" {
			return invite
		}
	}

	if flow, ok := payload["flow"].(map[string]any); ok {
		for _, key := range []string{"request_url", "return_to"} {
			if invite := extractInviteFromURL(getString(flow, key)); invite != "" {
				return invite
			}
		}
	}

	return findStringKey(payload, "invite")
}

func extractInviteFromURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	if invite := u.Query().Get("invite"); invite != "" {
		return strings.TrimSpace(invite)
	}
	if returnTo := u.Query().Get("return_to"); returnTo != "" {
		return extractInviteFromURL(returnTo)
	}
	return ""
}

func getString(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return value
	}
	return ""
}

func extractIdentityID(payload map[string]any) string {
	identity, ok := payload["identity"].(map[string]any)
	if !ok {
		return ""
	}
	if id, ok := identity["id"].(string); ok {
		return id
	}
	return ""
}

func extractKratosWebhookEmail(payload map[string]any) string {
	if identity, ok := payload["identity"].(map[string]any); ok {
		if traits, ok := identity["traits"].(map[string]any); ok {
			if email, ok := traits["email"].(string); ok {
				return email
			}
		}
	}
	if traits, ok := payload["traits"].(map[string]any); ok {
		if email, ok := traits["email"].(string); ok {
			return email
		}
	}
	if email, ok := payload["email"].(string); ok {
		return email
	}
	return findStringKey(payload, "email")
}

func findStringKey(value any, key string) string {
	switch v := value.(type) {
	case map[string]any:
		if s, ok := v[key].(string); ok {
			return s
		}
		for _, child := range v {
			if s := findStringKey(child, key); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range v {
			if s := findStringKey(child, key); s != "" {
				return s
			}
		}
	}
	return ""
}
