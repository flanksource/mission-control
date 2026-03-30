package oidc

import (
	"net/http"
	"net/url"
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

func mountOAuthRoutes(e *echo.Echo, oidcIssuer string) {
	// RFC 9728 OAuth 2.0 Protected Resource Metadata for MCP/OAuth clients.
	prmHandler := oauthProtectedResourceMetadataHandler(oidcIssuer)
	e.GET("/.well-known/oauth-protected-resource", prmHandler)
	e.GET("/.well-known/oauth-protected-resource/*", prmHandler)
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

		if resourcePath == "" || resourcePath == "/" {
			resourcePath = "/mcp"
		}
		if !strings.HasPrefix(resourcePath, "/") {
			resourcePath = "/" + resourcePath
		}

		metadata := oauthProtectedResourceMetadata{
			Resource:             origin + resourcePath,
			AuthorizationServers: []string{issuerWithOrigin(fallbackIssuer, origin)},
			ScopesSupported:      []string{"openid", "profile", "email"},
			BearerMethods:        []string{"header"},
		}

		return c.JSON(http.StatusOK, metadata)
	}
}

// issuerWithOrigin replaces the scheme+host of issuerURL with the detected origin,
// preserving any path component (e.g. "/oidc") required for RFC 9728 authorization_servers.
func issuerWithOrigin(issuerURL, origin string) string {
	u, err := url.Parse(issuerURL)
	if err != nil || u.Path == "" || u.Path == "/" {
		return origin
	}
	return origin + u.Path
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
