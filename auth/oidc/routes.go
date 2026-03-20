package oidc

import (
	"net/http"

	"github.com/flanksource/duty/context"
	"github.com/labstack/echo/v4"
	"github.com/tg123/go-htpasswd"
)

// MountRoutes sets up OIDC endpoints on the echo server.
// Called from auth.Middleware when OIDC is enabled.
func MountRoutes(e *echo.Echo, ctx context.Context, issuerURL, signingKeyPath, htpasswdFile string, lookup personLookup) error {
	provider, err := NewProvider(ctx, issuerURL, signingKeyPath)
	if err != nil {
		return err
	}


	checker, err := htpasswd.New(htpasswdFile, htpasswd.DefaultSystems, nil)
	if err != nil {
		return err
	}

	loginHandler := NewLoginHandler(provider.Storage, provider.OpenIDProvider, checker, lookup)

	e.GET("/oidc/login", loginHandler.ShowForm)
	e.POST("/oidc/login", loginHandler.HandleSubmit)

	// Mount the zitadel OP handler under /oidc (handles authorize, token, userinfo, etc.)
	oidcHandler := http.StripPrefix("/oidc", provider.Handler)
	e.Any("/oidc/*", echo.WrapHandler(oidcHandler))

	// Well-known endpoints (discovery, jwks)
	e.Any("/.well-known/*", echo.WrapHandler(provider.Handler))

	return nil
}
