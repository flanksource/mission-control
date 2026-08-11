package oidc

// Consent for dynamically registered clients.
//
// Registration is open, so anyone can mint a client_id whose redirect URI points
// at a host they control and send a victim an authorization link on Mission
// Control's own domain. PKCE does not help — the attacker is the client and
// holds the verifier. The consent screen is the only point in the flow where a
// human sees where the authorization code is about to be sent.
//
// The built-in mc-cli client skips consent: the user started it from their own
// terminal and its redirect URIs are fixed at compile time.

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/auth/oidc/static"
	"github.com/labstack/echo/v4"
	"github.com/zitadel/oidc/v3/pkg/op"
)

// requiresConsent reports whether an authorization for clientID must be
// explicitly approved by the user.
func requiresConsent(clientID string) bool {
	return clientID != ClientID
}

// consentRedirectURL is where the login handlers send the browser once the user
// has authenticated but before the authorization code is issued.
func consentRedirectURL(authRequestID string) string {
	return "/oidc/consent?auth_request_id=" + authRequestID
}

func (h *LoginHandler) ShowConsent(c echo.Context) error {
	id := c.QueryParam("auth_request_id")
	if id == "" {
		return c.String(http.StatusBadRequest, "missing auth_request_id")
	}
	if err := h.validateTransaction(c, id); err != nil {
		return c.String(http.StatusUnauthorized, "invalid oidc transaction")
	}

	ctx := c.Request().Context().(context.Context)

	var authRequest AuthRequest
	if err := ctx.DB().Where("id = ? AND expires_at > NOW()", id).First(&authRequest).Error; err != nil {
		return c.String(http.StatusBadRequest, "authorization request not found or expired")
	}
	if authRequest.Subject == "" {
		return c.String(http.StatusUnauthorized, "not authenticated")
	}

	clientName := authRequest.ClientID
	if metadata, err := decodeClientID(authRequest.ClientID); err == nil {
		clientName = (&dynamicClient{id: authRequest.ClientID, metadata: metadata}).DisplayName()
	}

	var person models.Person
	subject := authRequest.Subject
	if err := ctx.DB().Where("id = ?", authRequest.Subject).First(&person).Error; err == nil {
		if person.Email != "" {
			subject = person.Email
		} else if person.Name != "" {
			subject = person.Name
		}
	}

	scopes := strings.Join(authRequest.Scopes, ", ")
	if scopes == "" {
		scopes = "openid"
	}

	return c.HTML(http.StatusOK, fmt.Sprintf(static.ConsentHTML,
		html.EscapeString(clientName),
		html.EscapeString(subject),
		html.EscapeString(authRequest.RedirectURI),
		html.EscapeString(scopes),
		html.EscapeString(id),
		html.EscapeString(id),
	))
}

func (h *LoginHandler) HandleConsent(c echo.Context) error {
	id := c.FormValue("auth_request_id")
	if id == "" {
		return c.String(http.StatusBadRequest, "missing auth_request_id")
	}
	if err := h.validateTransaction(c, id); err != nil {
		return c.String(http.StatusUnauthorized, "invalid oidc transaction")
	}

	ctx := c.Request().Context().(context.Context)

	if c.FormValue("decision") != "allow" {
		// Drop the authorization request outright so the code can never be
		// issued, then let the client's own timeout surface the refusal.
		if err := h.storage.DeleteAuthRequest(c.Request().Context(), id); err != nil {
			ctx.Logger.Errorf("failed to delete denied auth request %s: %v", id, err)
		}
		return c.HTML(http.StatusOK, "<p>Authorization denied. You can close this window.</p>")
	}

	var authRequest AuthRequest
	if err := ctx.DB().Where("id = ? AND expires_at > NOW()", id).First(&authRequest).Error; err != nil {
		return c.String(http.StatusBadRequest, "authorization request not found or expired")
	}
	if authRequest.Subject == "" {
		return c.String(http.StatusUnauthorized, "not authenticated")
	}

	issuerCtx := op.ContextWithIssuer(c.Request().Context(), h.issuerURL)
	return c.Redirect(http.StatusFound, op.AuthCallbackURL(h.provider)(issuerCtx, id))
}

// completeLogin finishes an authorization once the user's identity is known,
// diverting through the consent screen for dynamically registered clients.
func (h *LoginHandler) completeLogin(c echo.Context, authRequestID, personID string) error {
	if err := h.storage.SetAuthRequestSubject(authRequestID, personID); err != nil {
		return err
	}

	ctx := c.Request().Context().(context.Context)

	var authRequest AuthRequest
	if err := ctx.DB().Where("id = ?", authRequestID).First(&authRequest).Error; err != nil {
		return err
	}

	if requiresConsent(authRequest.ClientID) {
		return c.Redirect(http.StatusFound, consentRedirectURL(authRequestID))
	}

	issuerCtx := op.ContextWithIssuer(c.Request().Context(), h.issuerURL)
	return c.Redirect(http.StatusFound, op.AuthCallbackURL(h.provider)(issuerCtx, authRequestID))
}
