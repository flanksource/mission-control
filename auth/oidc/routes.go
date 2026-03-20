package oidc

import (
	"net/http"

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

	e.GET("/oidc/login", loginHandler.ShowForm)
	e.POST("/oidc/login", loginHandler.HandleSubmit)

	oidcHandler := http.StripPrefix("/oidc", provider.Handler)
	e.Any("/oidc/*", echo.WrapHandler(oidcHandler))

	e.Any("/.well-known/*", echo.WrapHandler(provider.Handler))

	return nil
}
