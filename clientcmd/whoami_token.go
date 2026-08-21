package clientcmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/flanksource/incident-commander/auth/oidcclient"
)

func oidcServerCandidates(server string) []string {
	server = strings.TrimRight(server, "/")
	candidates := []string{server}
	if strings.HasSuffix(server, "/api") {
		candidates = append(candidates, strings.TrimSuffix(server, "/api"))
	}
	return uniqueStrings(candidates)
}

func oidcTokenExpiring(tokens *oidcclient.Tokens) bool {
	if tokens == nil {
		return false
	}
	return tokens.AccessToken == "" || (!tokens.ExpiresAt.IsZero() && time.Until(tokens.ExpiresAt) < time.Minute)
}

func refreshOIDCTokens(server string, tokens *oidcclient.Tokens) (*oidcclient.Tokens, error) {
	if tokens == nil {
		return nil, fmt.Errorf("no OIDC tokens")
	}
	if tokens.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token")
	}

	var lastErr error
	for _, candidate := range oidcServerCandidates(server) {
		endpoints, err := oidcclient.Discover(strings.TrimRight(candidate, "/") + "/.well-known/openid-configuration")
		if err != nil {
			lastErr = err
			continue
		}
		refreshed, err := oidcclient.RefreshToken(endpoints.TokenEndpoint, tokens.RefreshToken)
		if err != nil {
			lastErr = err
			continue
		}
		if refreshed.RefreshToken == "" {
			refreshed.RefreshToken = tokens.RefreshToken
		}
		if refreshed.IDToken == "" {
			refreshed.IDToken = tokens.IDToken
		}
		return refreshed, nil
	}
	return nil, lastErr
}

func updateContextOIDCTokens(cfg *MCConfig, name string, tokens *oidcclient.Tokens) {
	if cfg == nil || name == "" || tokens == nil {
		return
	}
	ctx := cfg.GetContext(name)
	if ctx == nil {
		return
	}
	ctx.SetOIDCTokens(tokens)
	if err := SaveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "failed to update context OIDC tokens: %v\n", err)
	}
}
