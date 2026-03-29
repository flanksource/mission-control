package oidc

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/auth/oidc/static"
	"github.com/labstack/echo/v4"
)

// MountRoutes sets up OIDC endpoints on the echo server.
// The OIDC provider is mounted under /oidc/ with the issuer set to {issuerURL}/oidc
// so that discovery at /oidc/.well-known/openid-configuration returns correct endpoint URLs.
// A convenience redirect from /.well-known/openid-configuration is provided for standard discovery.
func MountRoutes(e *echo.Echo, ctx context.Context, issuerURL, signingKeyPath string, checker CredentialChecker, lookup PersonLookup) error {
	oidcIssuer := strings.TrimRight(issuerURL, "/")
	provider, err := NewProvider(ctx, oidcIssuer, signingKeyPath)
	if err != nil {
		return err
	}

	loginHandler := NewLoginHandler(provider.Storage, provider.OpenIDProvider, checker, lookup, oidcIssuer)

	// Custom login form (not part of the standard OIDC protocol paths).
	e.GET("/oidc/login", loginHandler.ShowForm)
	e.POST("/oidc/login", loginHandler.HandleSubmit)

	// Standard OIDC protocol endpoints — mounted at the root so that the issuer URL
	// and the authorization_endpoint/token_endpoint values in the discovery document
	// resolve to real routes on this server.
	h := echo.WrapHandler(provider.Handler)
	e.Any("/authorize", h)
	e.Any("/authorize/*", h)
	e.Any("/oauth/token", h)
	e.Any("/oauth/introspect", h)
	e.Any("/userinfo", h)
	e.Any("/keys", h)
	e.Any("/endsession", h)
	e.Any("/.well-known/*", h)

	// Serve embedded static assets (logo, tailwind)
	staticFS, _ := fs.Sub(static.FS, ".")
	staticHandler := http.StripPrefix("/oidc/static/", http.FileServer(http.FS(staticFS)))
	e.GET("/oidc/static/*", echo.WrapHandler(staticHandler))

	return nil
}
