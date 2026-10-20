package connection

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
)

// The AI chat calls the default LLM provider (--llm-connection) through Mission Control, which adds the provider's
// credentials, so they never leave the server. Chatting needs mcp:use, as the chat's tools are the user's MCP calls.
// The provider call itself isn't checked as the user: the operator chose the connection with a server flag, and the
// user only ever reaches that one connection, never one named in the request.
func registerLLMRoutes(e *echo.Echo) {
	g := e.Group("/llm", rbac.Authorization(policy.ObjectMCP, policy.ActionMCPUse))
	g.GET("/provider", GetLLMProvider)
	g.POST("/proxy/*", ProxyLLM)
}

// defaultLLMBaseURLs are the APIs the provider SDKs call when the connection has no URL.
var defaultLLMBaseURLs = map[string]string{
	models.ConnectionTypeOpenAI:    "https://api.openai.com/v1",
	models.ConnectionTypeAnthropic: "https://api.anthropic.com/v1",
}

var errLLMUnavailable = dutyAPI.Errorf(dutyAPI.EINTERNAL, "the AI assistant isn't available")

// LLMProvider tells the chat which provider SDK and model to use. It carries no credentials.
type LLMProvider struct {
	Type  string `json:"type"`
	Model string `json:"model,omitempty"`
}

// GetLLMProvider returns the default LLM provider's type and model.
func GetLLMProvider(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	conn, err := defaultLLMConnection(ctx)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, LLMProvider{Type: conn.Type, Model: conn.Properties["model"]})
}

// ProxyLLM forwards a provider API request to the default LLM provider with its credentials. The path after
// /llm/proxy is relative to the provider's base URL, e.g. /llm/proxy/messages for Anthropic's /v1/messages.
func ProxyLLM(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	conn, err := defaultLLMConnection(ctx)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	baseURL := lo.CoalesceOrEmpty(conn.URL, defaultLLMBaseURLs[conn.Type])
	target, err := url.Parse(baseURL)
	if err != nil {
		ctx.Logger.Errorf("invalid URL on the default LLM connection: %v", err)
		return dutyAPI.WriteError(c, errLLMUnavailable)
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL.Scheme = target.Scheme
			r.Out.URL.Host = target.Host
			r.Out.URL.Path = strings.TrimSuffix(target.Path, "/") + "/" + strings.TrimPrefix(c.Param("*"), "/")
			r.Out.URL.RawPath = ""
			r.Out.Host = target.Host

			// The user's session and whatever key the SDK was given stay here
			r.Out.Header.Del("Cookie")
			r.Out.Header.Del("Authorization")
			r.Out.Header.Del("X-Api-Key")
			switch conn.Type {
			case models.ConnectionTypeAnthropic:
				r.Out.Header.Set("X-Api-Key", conn.Password)
			default:
				r.Out.Header.Set("Authorization", "Bearer "+conn.Password)
			}
		},
		// Stream responses as they arrive
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			ctx.Logger.Errorf("failed to reach the LLM provider: %v", err)
			http.Error(w, errLLMUnavailable.Error(), http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(c.Response(), c.Request())
	return nil
}

// defaultLLMConnection hydrates the connection set by --llm-connection without checking connection:use, like the
// default SMTP connection for system email. It's only for the routes above: setting the flag is what grants its use,
// and callers never get the credentials. Failures are logged, not returned, so the chat doesn't reveal the provider.
func defaultLLMConnection(ctx context.Context) (*models.Connection, error) {
	conn, err := db.FindDefaultLLMProviderConnection(ctx)
	if err != nil {
		ctx.Logger.Errorf("failed to find the default LLM connection: %v", err)
		if dutyAPI.ErrorCode(err) == dutyAPI.ENOTFOUND {
			return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "the AI assistant isn't configured")
		}
		return nil, errLLMUnavailable
	}

	if _, ok := defaultLLMBaseURLs[conn.Type]; !ok {
		ctx.Logger.Errorf("the default LLM connection has unsupported type %q", conn.Type)
		return nil, errLLMUnavailable
	}

	hydrated, err := ctx.HydrateConnection(conn)
	if err != nil {
		ctx.Logger.Errorf("failed to hydrate the default LLM connection: %v", err)
		return nil, errLLMUnavailable
	}

	return hydrated, nil
}
