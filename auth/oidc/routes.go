package oidc

import (
	"github.com/flanksource/duty/context"
	"github.com/labstack/echo/v4"
)

// MountRoutes sets up OIDC endpoints on the echo server.
func MountRoutes(e *echo.Echo, ctx context.Context, issuerURL, signingKeyPath string, checker CredentialChecker, lookup PersonLookup) error {
	provider, err := NewProvider(ctx, issuerURL, signingKeyPath)
	if err != nil {
		return err
	}

	loginHandler := NewLoginHandler(provider.Storage, provider.OpenIDProvider, checker, lookup)

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

	return nil
}
