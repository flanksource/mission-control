package oidc

import (
	"fmt"
	"html"
	"net/http"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/auth/oidc/static"
	"github.com/labstack/echo/v4"
	"github.com/zitadel/oidc/v3/pkg/op"
)

type LoginHandler struct {
	storage      *Storage
	provider     op.OpenIDProvider
	checker      CredentialChecker
	PersonLookup PersonLookup
	issuerURL    string
}

type CredentialChecker interface {
	Match(ctx context.Context, user, pass string) error
}

type LoginRedirector interface {
	LoginRedirectURL(authRequestID string) (string, error)
}

type CallbackSubjectResolver interface {
	CallbackSubject(c echo.Context) (string, error)
}

type PersonLookup func(ctx context.Context, user string) (personID string, err error)

func NewLoginHandler(storage *Storage, provider op.OpenIDProvider, checker CredentialChecker, lookup PersonLookup, issuerURL string) *LoginHandler {
	return &LoginHandler{
		storage:      storage,
		provider:     provider,
		checker:      checker,
		PersonLookup: lookup,
		issuerURL:    issuerURL,
	}
}

func (h *LoginHandler) ShowForm(c echo.Context) error {
	id := c.QueryParam("auth_request_id")
	if id == "" {
		return c.String(http.StatusBadRequest, "missing auth_request_id")
	}

	if redirector, ok := h.checker.(LoginRedirector); ok {
		redirectURL, err := redirector.LoginRedirectURL(id)
		if err != nil {
			return c.String(http.StatusInternalServerError, "failed to build login redirect")
		}
		return c.Redirect(http.StatusFound, redirectURL)
	}

	return c.HTML(http.StatusOK, fmt.Sprintf(static.LoginHTML, html.EscapeString(id), ""))
}

func (h *LoginHandler) HandleSubmit(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	id := c.FormValue("auth_request_id")
	username := c.FormValue("username")
	password := c.FormValue("password")

	renderForm := func(msg string) error {
		return c.HTML(http.StatusOK, fmt.Sprintf(static.LoginHTML, html.EscapeString(id),
			`<p class="mt-3 text-sm text-red-600">`+html.EscapeString(msg)+`</p>`))
	}

	if id == "" || username == "" || password == "" {
		return renderForm("All fields required")
	}

	if err := h.checker.Match(ctx, username, password); err != nil {
		return renderForm(fmt.Sprintf("Invalid credentials: %v", err))
	}

	personID, err := h.PersonLookup(ctx, username)
	if err != nil {
		return renderForm("User not found")
	}

	if err := h.storage.SetAuthRequestSubject(id, personID); err != nil {
		return renderForm("Internal error")
	}

	issuerCtx := op.ContextWithIssuer(c.Request().Context(), h.issuerURL)
	callbackURL := op.AuthCallbackURL(h.provider)(issuerCtx, id)
	return c.Redirect(http.StatusFound, callbackURL)
}

func (h *LoginHandler) HandleExternalCallback(c echo.Context) error {
	id := c.QueryParam("auth_request_id")
	if id == "" {
		return c.String(http.StatusBadRequest, "missing auth_request_id")
	}

	resolver, ok := h.checker.(CallbackSubjectResolver)
	if !ok {
		return c.String(http.StatusNotFound, "external callback is not configured")
	}

	personID, err := resolver.CallbackSubject(c)
	if err != nil {
		return c.String(http.StatusUnauthorized, "authorization failed")
	}

	if err := h.storage.SetAuthRequestSubject(id, personID); err != nil {
		return c.String(http.StatusInternalServerError, "internal error")
	}

	issuerCtx := op.ContextWithIssuer(c.Request().Context(), h.issuerURL)
	callbackURL := op.AuthCallbackURL(h.provider)(issuerCtx, id)
	return c.Redirect(http.StatusFound, callbackURL)
}
