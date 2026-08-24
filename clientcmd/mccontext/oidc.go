package mccontext

import (
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/auth/oidcclient"
)

// OIDCServerCandidates lists the URLs worth attempting provider discovery
// against: a server recorded as the frontend's /api path publishes its
// well-known document at the root too.
func OIDCServerCandidates(server string) []string {
	server = strings.TrimRight(server, "/")
	candidates := []string{server}
	if strings.HasSuffix(server, "/api") {
		candidates = append(candidates, strings.TrimSuffix(server, "/api"))
	}
	return uniqueStrings(candidates)
}

// OIDCTokenExpiring reports whether a token set is unusable or about to be.
func OIDCTokenExpiring(tokens *oidcclient.Tokens) bool {
	if tokens == nil {
		return false
	}
	return tokens.AccessToken == "" || (!tokens.ExpiresAt.IsZero() && time.Until(tokens.ExpiresAt) < time.Minute)
}

// DiscoverOIDCEndpoints resolves the provider metadata for a server. Unlike the
// grant itself, discovery is idempotent and safe to attempt against every
// candidate — it spends nothing.
func DiscoverOIDCEndpoints(server string) (*oidcclient.Discovery, error) {
	var lastErr error
	for _, candidate := range OIDCServerCandidates(server) {
		endpoints, err := oidcclient.Discover(strings.TrimRight(candidate, "/") + "/.well-known/openid-configuration")
		if err == nil {
			return endpoints, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("OIDC discovery failed for %s: %w", server, lastErr)
}

// contextTokenEndpoint returns the token endpoint for a context, discovering
// and caching it in config.json on first use so later commands skip the two
// discovery round-trips. Failing to write that cache is not fatal — it holds no
// credential, and the refresh it guards is already in flight.
func contextTokenEndpoint(cfg *MCConfig, mcCtx *MCContext) (string, error) {
	if mcCtx.Endpoints != nil && mcCtx.Endpoints.TokenEndpoint != "" {
		return mcCtx.Endpoints.TokenEndpoint, nil
	}

	endpoints, err := DiscoverOIDCEndpoints(mcCtx.Server)
	if err != nil {
		return "", err
	}
	mcCtx.Endpoints = endpoints

	if stored := cfg.GetContext(mcCtx.Name); stored != nil {
		stored.Endpoints = endpoints
		if err := saveConfigLocked(cfg); err != nil {
			logger.Debugf("failed to cache OIDC endpoints for context %q: %v", mcCtx.Name, err)
		}
	}
	return endpoints.TokenEndpoint, nil
}
