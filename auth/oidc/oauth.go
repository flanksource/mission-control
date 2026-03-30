package oidc

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

const oauthProtectedResourcePrefix = "/.well-known/oauth-protected-resource"

type oauthProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers,omitempty"`
	ScopesSupported      []string `json:"scopes_supported,omitempty"`
	BearerMethods        []string `json:"bearer_methods_supported,omitempty"`
}

type oauthAuthorizationServerMetadata struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	RegistrationEndpoint          string   `json:"registration_endpoint,omitempty"`
	ResponseTypesSupported        []string `json:"response_types_supported"`
	GrantTypesSupported           []string `json:"grant_types_supported,omitempty"`
	TokenEndpointAuthMethods      []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported,omitempty"`
	ScopesSupported               []string `json:"scopes_supported,omitempty"`
}

func mountOAuthRoutes(e *echo.Echo, oidcIssuer string) {
	// OAuth 2.0 Authorization Server Metadata (RFC 8414).
	e.GET("/.well-known/oauth-authorization-server", oauthAuthorizationServerMetadataHandler(oidcIssuer))

	// RFC 9728 OAuth 2.0 Protected Resource Metadata for MCP/OAuth clients.
	prmHandler := oauthProtectedResourceMetadataHandler(oidcIssuer)
	e.GET("/.well-known/oauth-protected-resource", prmHandler)
	e.GET("/.well-known/oauth-protected-resource/*", prmHandler)
}

func detectRequestOrigin(c echo.Context, fallbackIssuer string) string {
	proto := c.Request().Header.Get("X-Forwarded-Proto")
	if proto == "" {
		if c.Scheme() != "" {
			proto = c.Scheme()
		} else {
			proto = "http"
		}
	}

	host := c.Request().Header.Get("X-Forwarded-Host")
	if host == "" {
		host = c.Request().Host
	}

	if host == "" {
		return strings.TrimRight(fallbackIssuer, "/")
	}

	// X-Forwarded-Host can include a list; use the first value.
	if i := strings.Index(host, ","); i > -1 {
		host = strings.TrimSpace(host[:i])
	}

	return strings.TrimRight(proto+"://"+host, "/")
}

func oauthProtectedResourceMetadataHandler(issuerURL string) echo.HandlerFunc {
	fallbackIssuer := strings.TrimRight(issuerURL, "/")

	return func(c echo.Context) error {
		origin := detectRequestOrigin(c, fallbackIssuer)
		requestedPath := c.Request().URL.Path

		resourcePath := ""
		if requestedPath != oauthProtectedResourcePrefix {
			resourcePath = strings.TrimPrefix(requestedPath, oauthProtectedResourcePrefix)
		}

		if resourcePath == "" {
			resourcePath = "/mcp"
		}
		if !strings.HasPrefix(resourcePath, "/") {
			resourcePath = "/" + resourcePath
		}

		metadata := oauthProtectedResourceMetadata{
			Resource:             origin + resourcePath,
			AuthorizationServers: []string{origin},
			ScopesSupported:      []string{"openid", "profile", "email", "offline_access"},
			BearerMethods:        []string{"header"},
		}

		return c.JSON(http.StatusOK, metadata)
	}
}

func oauthAuthorizationServerMetadataHandler(issuerURL string) echo.HandlerFunc {
	fallbackIssuer := strings.TrimRight(issuerURL, "/")

	return func(c echo.Context) error {
		issuer := detectRequestOrigin(c, fallbackIssuer)
		metadata := oauthAuthorizationServerMetadata{
			Issuer:                        issuer,
			AuthorizationEndpoint:         issuer + "/authorize",
			TokenEndpoint:                 issuer + "/oauth/token",
			ResponseTypesSupported:        []string{"code"},
			GrantTypesSupported:           []string{"authorization_code", "refresh_token"},
			TokenEndpointAuthMethods:      []string{"none"},
			CodeChallengeMethodsSupported: []string{"S256"},
			ScopesSupported:               []string{"openid", "profile", "email", "offline_access"},
		}

		return c.JSON(http.StatusOK, metadata)
	}
}
