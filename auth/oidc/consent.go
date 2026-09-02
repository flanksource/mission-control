package oidc

// Consent for DCR and CIMD clients.
//
// Client metadata is caller-controlled, so anyone can name a redirect URI they
// control and send a victim an authorization link on Mission Control's domain.
// PKCE does not help — the attacker is the client and holds the verifier. The
// consent screen is where a human sees where the authorization code will be sent.
//
// The built-in mc-cli client skips consent: the user started it from their own
// terminal and its redirect URIs are fixed at compile time.

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
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
	if err := ctx.DB().Where("id = ? AND expires_at > NOW() AND done = FALSE", id).First(&authRequest).Error; err != nil {
		return c.String(http.StatusBadRequest, "authorization request not found or expired")
	}
	if authRequest.Subject == "" {
		return c.String(http.StatusUnauthorized, "not authenticated")
	}

	clientName := authRequest.ClientID
	if IsMetadataClient(authRequest.ClientID) {
		client, err := h.storage.GetClientByClientID(c.Request().Context(), authRequest.ClientID)
		if err != nil {
			return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
		}
		if err := op.ValidateAuthReqRedirectURI(client, authRequest.RedirectURI, authRequest.GetResponseType()); err != nil {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "redirect URI is no longer registered"))
		}
		if namedClient, ok := client.(interface{ DisplayName() string }); ok {
			clientName = namedClient.DisplayName()
		}
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

	var authRequest AuthRequest
	if err := ctx.DB().Where("id = ? AND expires_at > NOW() AND done = FALSE", id).First(&authRequest).Error; err != nil {
		return c.String(http.StatusBadRequest, "authorization request not found or expired")
	}
	if authRequest.Subject == "" {
		return c.String(http.StatusUnauthorized, "not authenticated")
	}

	if c.FormValue("decision") != "allow" {
		redirectURL, err := h.denialRedirectURL(c, &authRequest)
		if err != nil {
			return c.String(http.StatusBadRequest, "invalid client redirect URI")
		}
		if err := h.storage.DeleteAuthRequest(c.Request().Context(), id); err != nil {
			ctx.Logger.Errorf("failed to delete denied auth request %s: %v", id, err)
			return c.String(http.StatusInternalServerError, "failed to deny authorization")
		}
		if h.oidcTxCookieValidator != nil {
			h.oidcTxCookieValidator.clearCookie(c.Response(), id)
		}
		return c.Redirect(http.StatusFound, redirectURL)
	}

	if err := h.storage.CompleteAuthRequest(id); err != nil {
		return c.String(http.StatusBadRequest, "authorization request is no longer pending")
	}

	issuerCtx := op.ContextWithIssuer(c.Request().Context(), h.issuerURL)
	return c.Redirect(http.StatusFound, op.AuthCallbackURL(h.provider)(issuerCtx, id))
}

// denialRedirectURL revalidates the stored redirect before returning an OAuth error.
func (h *LoginHandler) denialRedirectURL(c echo.Context, authRequest *AuthRequest) (string, error) {
	client, err := h.storage.GetClientByClientID(c.Request().Context(), authRequest.ClientID)
	if err != nil || client == nil {
		return "", fmt.Errorf("client is not registered")
	}
	if err := op.ValidateAuthReqRedirectURI(client, authRequest.RedirectURI, authRequest.GetResponseType()); err != nil {
		return "", fmt.Errorf("redirect URI is not registered: %w", err)
	}

	redirectURL, err := url.Parse(authRequest.RedirectURI)
	if err != nil {
		return "", err
	}
	query := redirectURL.Query()
	query.Set("error", "access_denied")
	if authRequest.State != "" {
		query.Set("state", authRequest.State)
	}
	redirectURL.RawQuery = query.Encode()
	return redirectURL.String(), nil
}

// completeLogin finishes an authorization once the user's identity is known,
// diverting through the consent screen for metadata-backed clients.
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
	if err := h.storage.CompleteAuthRequest(authRequestID); err != nil {
		return err
	}

	issuerCtx := op.ContextWithIssuer(c.Request().Context(), h.issuerURL)
	return c.Redirect(http.StatusFound, op.AuthCallbackURL(h.provider)(issuerCtx, authRequestID))
}
