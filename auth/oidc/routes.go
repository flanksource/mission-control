package oidc

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/auth/oidc/static"
	"github.com/flanksource/incident-commander/auth/signing"
	"github.com/labstack/echo/v4"
)

// MountRoutes sets up OIDC endpoints on the echo server.
// The OIDC provider is mounted under /oidc/ with the issuer set to {issuerURL}/oidc
// so that discovery at /oidc/.well-known/openid-configuration returns correct endpoint URLs.
// A convenience redirect from /.well-known/openid-configuration is provided for standard discovery.
func MountRoutes(e *echo.Echo, ctx context.Context, issuerURL string, passwordChecker PasswordLoginChecker, externalProvider ExternalLoginProvider, lookup PersonLookup) error {
	oidcIssuer := strings.TrimRight(issuerURL, "/")
	cryptoKey, err := generateCryptoKey()
	if err != nil {
		return fmt.Errorf("oidc crypto key: %w", err)
	}

	privateKey, keyID, err := signing.PrivateKey()
	if err != nil {
		return fmt.Errorf("oidc signing key: %w", err)
	}

	provider, err := NewProvider(ctx, oidcIssuer, cryptoKey, privateKey, keyID)
	if err != nil {
		return err
	}

	oidcTxCookierManager := newTransactionCookieManager(cryptoKey, oidcIssuer)

	var loginHandler *LoginHandler
	if externalProvider != nil {
		loginHandler = NewExternalLoginHandler(provider.Storage, provider.OpenIDProvider, externalProvider, oidcIssuer)
	} else {
		loginHandler = NewPasswordLoginHandler(provider.Storage, provider.OpenIDProvider, passwordChecker, lookup, oidcIssuer)
	}
	loginHandler.oidcTxCookieValidator = oidcTxCookierManager

	// Custom login form (not part of the standard OIDC protocol paths).
	e.GET("/oidc/login", loginHandler.ShowForm)
	e.POST("/oidc/login", loginHandler.HandleSubmit)
	e.GET("/oidc/kratos/callback", loginHandler.HandleExternalCallback)
	e.GET("/oidc/clerk/callback", loginHandler.HandleExternalCallback)
	e.POST("/oidc/clerk/callback", loginHandler.HandleExternalCallback)

	// MCP Clients need OAuth well-known discovery endpoints (not just OIDC discovery).
	mountOAuthRoutes(e, oidcIssuer)

	// Standard OIDC protocol endpoints — mounted at the root so that the issuer URL
	// and the authorization_endpoint/token_endpoint values in the discovery document
	// resolve to real routes on this server.
	authorizeHandler := echo.WrapHandler(oidcTxCookierManager.issueOnAuthorize(provider.Handler))
	callbackHandler := echo.WrapHandler(oidcTxCookierManager.requireOnAuthorizeCallback(provider.Handler))
	h := echo.WrapHandler(provider.Handler)
	e.Any("/authorize", authorizeHandler)
	e.Any("/authorize/*", callbackHandler)
	e.Any("/oauth/token", h)
	e.Any("/oauth/introspect", h)
	e.Any("/userinfo", h)
	e.Any("/keys", h)
	e.Any("/endsession", h)
	e.Any("/.well-known/*", h)

	RegisterStaticAssets(e)

	return nil
}

// RegisterStaticAssets mounts /oidc/static/* so that shared login assets
// (logo.svg, tailwind.min.js) are reachable regardless of whether the full
// OIDC provider is enabled. Basic auth's login page depends on these too,
// so it calls this from auth.UseBasic.
func RegisterStaticAssets(e *echo.Echo) {
	staticFS, _ := fs.Sub(static.FS, ".")
	staticHandler := http.StripPrefix("/oidc/static/", http.FileServer(http.FS(staticFS)))
	e.GET("/oidc/static/*", echo.WrapHandler(staticHandler))
}
