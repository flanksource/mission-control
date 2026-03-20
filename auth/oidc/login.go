package oidc

import (
	"fmt"
	"html"
	"net/http"

	"github.com/flanksource/duty/context"
	"github.com/labstack/echo/v4"
	"github.com/zitadel/oidc/v3/pkg/op"
)

const loginFormHTML = `<!DOCTYPE html>
<html>
<head><title>Mission Control Login</title></head>
<body>
<h2>Sign in</h2>
<form method="POST" action="/oidc/login">
  <input type="hidden" name="auth_request_id" value="%s">
  <label>Username: <input type="text" name="username" autocomplete="username"></label><br>
  <label>Password: <input type="password" name="password" autocomplete="current-password"></label><br>
  <button type="submit">Sign in</button>
</form>
%s
</body>
</html>`

// LoginHandler handles the OIDC login form, delegating credential validation
// to the Basic auth checker and person lookup.
type LoginHandler struct {
	storage      *Storage
	provider     op.OpenIDProvider
	checker      CredentialChecker
	PersonLookup PersonLookup
}

// CredentialChecker validates username/password credentials.
type CredentialChecker interface {
	Match(user, pass string) bool
}

// PersonLookup finds a person by username/email, returning the person UUID.
type PersonLookup func(ctx context.Context, user string) (personID string, err error)

func NewLoginHandler(storage *Storage, provider op.OpenIDProvider, checker CredentialChecker, lookup PersonLookup) *LoginHandler {
	return &LoginHandler{
		storage:      storage,
		provider:     provider,
		checker:      checker,
		PersonLookup: lookup,
	}
}

func (h *LoginHandler) ShowForm(c echo.Context) error {
	id := c.QueryParam("auth_request_id")
	if id == "" {
		return c.String(http.StatusBadRequest, "missing auth_request_id")
	}
	return c.HTML(http.StatusOK, fmt.Sprintf(loginFormHTML, html.EscapeString(id), ""))
}

func (h *LoginHandler) HandleSubmit(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	id := c.FormValue("auth_request_id")
	username := c.FormValue("username")
	password := c.FormValue("password")

	renderForm := func(msg string) error {
		return c.HTML(http.StatusOK, fmt.Sprintf(loginFormHTML, html.EscapeString(id), "<p style='color:red'>"+msg+"</p>"))
	}

	if id == "" || username == "" || password == "" {
		return renderForm("All fields required")
	}

	if !h.checker.Match(username, password) {
		return renderForm("Invalid credentials")
	}

	personID, err := h.PersonLookup(ctx, username)
	if err != nil {
		return renderForm("User not found")
	}

	if err := h.storage.SetAuthRequestSubject(id, personID); err != nil {
		return renderForm("Internal error")
	}

	callbackURL := op.AuthCallbackURL(h.provider)(c.Request().Context(), id)
	return c.Redirect(http.StatusFound, callbackURL)
}
