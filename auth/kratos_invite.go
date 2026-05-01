package auth

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/incident-commander/api"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (k *KratosHandler) createInvite(ctx context.Context, req InviteUserRequest) (*models.Invite, error) {
	email := normalizeEmail(req.Email)
	if email == "" {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "email is required")
	}

	role := strings.TrimSpace(req.Role)
	if role == "" {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "role is required")
	}

	var existingID string
	if err := ctx.DB().Raw(`SELECT id FROM identities WHERE lower(traits->>'email') = ? LIMIT 1`, email).Scan(&existingID).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "error checking existing identity")
	}
	if existingID != "" {
		return nil, dutyAPI.Errorf(dutyAPI.ECONFLICT, "user already exists")
	}

	invite := &models.Invite{Email: email, Role: role}
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
		if err := rbac.AddRoleForUser(identityID, invite.Role); err != nil {
			return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "failed to assign invited role"))
		}
		if err := acceptKratosInvite(ctx, invite.ID.String()); err != nil {
			return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "failed to accept invite"))
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
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
