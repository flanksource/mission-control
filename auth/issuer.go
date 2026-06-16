package auth

import (
	"strings"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/vars"
)

// OIDCIssuerURL returns the base URL the embedded OIDC provider is served under.
//
// Single-tenant deployments (basic, kratos) only need the frontend exposed —
// it proxies all OIDC protocol endpoints (/authorize, /oauth/token,
// /.well-known/*, /oidc/*) to the backend, so the frontend URL is the issuer.
//
// Multi-tenant Clerk shares one frontend across tenants; unauthenticated
// protocol endpoints can't be routed per-tenant there, so the issuer stays
// on the per-tenant backend URL.
func OIDCIssuerURL() string {
	if vars.AuthMode != Clerk && api.FrontendURL != "" {
		return strings.TrimRight(api.FrontendURL, "/")
	}
	return strings.TrimRight(api.PublicURL, "/")
}
