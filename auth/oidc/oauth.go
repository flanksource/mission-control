package oidc

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

const (
	oauthProtectedResourcePrefix = "/.well-known/oauth-protected-resource"

	// openIDConfigurationPath is OIDC discovery; authorizationServerMetadataPath
	// is its RFC 8414 equivalent. MCP clients probe the latter first and the
	// zitadel router only serves the former, so both are mounted here.
	openIDConfigurationPath         = "/.well-known/openid-configuration"
	authorizationServerMetadataPath = "/.well-known/oauth-authorization-server"
)

type oauthProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers,omitempty"`
	ScopesSupported      []string `json:"scopes_supported,omitempty"`
	BearerMethods        []string `json:"bearer_methods_supported,omitempty"`
}

// mountOAuthCORS installs a credential-free policy before the server-wide CORS middleware.
func mountOAuthCORS(e *echo.Echo) {
	e.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !isOAuthCORSPath(c.Request().URL.Path) {
				return next(c)
			}

			setOAuthCORSHeaders(c.Response().Header())
			if c.Request().Method == http.MethodOptions {
				return c.NoContent(http.StatusNoContent)
			}
			return next(c)
		}
	})
}

func isOAuthCORSPath(path string) bool {
	return path == openIDConfigurationPath ||
		pathIsOrUnder(path, authorizationServerMetadataPath) ||
		pathIsOrUnder(path, oauthProtectedResourcePrefix) ||
		path == RegistrationEndpoint ||
		path == "/oauth/token" ||
		path == MCPResourcePath
}

func pathIsOrUnder(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func setOAuthCORSHeaders(header http.Header) {
	header.Set("Access-Control-Allow-Origin", "*")
	header.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	header.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, MCP-Protocol-Version, MCP-Session-Id, Last-Event-ID")
	header.Set("Access-Control-Expose-Headers", "WWW-Authenticate, MCP-Session-Id")
	header.Del("Access-Control-Allow-Credentials")
}

func mountOAuthRoutes(e *echo.Echo, oidcIssuer string, providerHandler http.Handler) {
	// RFC 9728 OAuth 2.0 Protected Resource Metadata for MCP/OAuth clients.
	prmHandler := oauthProtectedResourceMetadataHandler(oidcIssuer)
	e.GET(oauthProtectedResourcePrefix, prmHandler)
	e.GET(oauthProtectedResourcePrefix+"/*", prmHandler)

	// The zitadel provider builds the discovery document but has no hook for MCP
	// client registration capabilities, so the response is augmented on the way
	// out. The same document answers RFC 8414, which zitadel does not serve at all.
	metadata := authorizationServerMetadataHandler(providerHandler)
	e.GET(openIDConfigurationPath, metadata)
	e.GET(authorizationServerMetadataPath, metadata)
	e.GET(authorizationServerMetadataPath+"/*", metadata)
}

// authorizationServerMetadataHandler adds CIMD and DCR capabilities to discovery.
func authorizationServerMetadataHandler(providerHandler http.Handler) echo.HandlerFunc {
	return func(c echo.Context) error {
		req := c.Request().Clone(c.Request().Context())
		req.URL.Path = openIDConfigurationPath
		req.Method = http.MethodGet

		rec := &bufferedResponseWriter{headers: http.Header{}, status: http.StatusOK}
		providerHandler.ServeHTTP(rec, req)

		var doc map[string]any
		if rec.status != http.StatusOK || json.Unmarshal(rec.body.Bytes(), &doc) != nil {
			// Pass the provider's response through untouched rather than
			// masking whatever went wrong behind a synthetic document.
			for k, values := range rec.headers {
				for _, v := range values {
					c.Response().Header().Add(k, v)
				}
			}
			setOAuthCORSHeaders(c.Response().Header())
			return c.Blob(rec.status, rec.headers.Get(echo.HeaderContentType), rec.body.Bytes())
		}

		issuer, _ := doc["issuer"].(string)
		if issuer == "" {
			issuer = detectRequestOrigin(c, "")
		}
		doc["registration_endpoint"] = strings.TrimRight(issuer, "/") + RegistrationEndpoint
		doc["client_id_metadata_document_supported"] = true

		// Discovery documents are public and cookie-free; browser-based clients
		// such as the MCP Inspector fetch them cross-origin.
		setOAuthCORSHeaders(c.Response().Header())
		return c.JSON(http.StatusOK, doc)
	}
}

// bufferedResponseWriter captures a handler's response so it can be rewritten.
type bufferedResponseWriter struct {
	headers http.Header
	body    bytes.Buffer
	status  int
	written bool
}

func (w *bufferedResponseWriter) Header() http.Header { return w.headers }

func (w *bufferedResponseWriter) WriteHeader(status int) {
	if !w.written {
		w.status = status
		w.written = true
	}
}

func (w *bufferedResponseWriter) Write(b []byte) (int, error) {
	w.written = true
	return w.body.Write(b)
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

		setOAuthCORSHeaders(c.Response().Header())
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
