package cmd

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/sdk"
)

func newAPIClient(mcCtx *MCContext, opts ...sdk.ClientOption) *sdk.Client {
	if mcCtx == nil {
		return nil
	}
	opts = append([]sdk.ClientOption{sdk.WithTokenProvider(contextTokenProvider(mcCtx))}, opts...)
	return newAPIClientForServer(mcCtx.Server, mcCtx.Token, opts...)
}

func newAPIClientForServer(server, token string, opts ...sdk.ClientOption) *sdk.Client {
	return sdk.New(server, token, opts...)
}

func contextTokenProvider(mcCtx *MCContext) sdk.TokenProvider {
	var mu sync.Mutex
	return func(context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return resolveContextToken(mcCtx)
	}
}

func resolveContextToken(mcCtx *MCContext) (string, error) {
	if mcCtx == nil {
		return "", nil
	}
	token := mcCtx.Token
	if mcCtx.Server == "" {
		return token, nil
	}

	stored, err := loadStoredOIDCTokens(mcCtx.Server)
	if err != nil {
		if token != "" || os.IsNotExist(err) {
			return token, nil
		}
		return "", fmt.Errorf("failed to load stored tokens for %s: %w", mcCtx.Server, err)
	}
	if stored == nil || stored.Tokens == nil {
		return token, nil
	}

	previousStoredToken := stored.Tokens.AccessToken
	if shouldRefreshStoredToken(token, previousStoredToken, stored) {
		logger.Debugf("refreshing OIDC token for context %q server %s", mcCtx.Name, mcCtx.Server)
		refreshed, err := refreshOIDCTokens(mcCtx.Server, stored)
		if err != nil {
			logger.Debugf("failed to refresh OIDC token for context %q server %s: %v", mcCtx.Name, mcCtx.Server, err)
			if token != "" && token != previousStoredToken {
				return token, nil
			}
			return "", fmt.Errorf("refresh OIDC token for %s: %w", mcCtx.Server, err)
		}
		logger.Debugf("refreshed OIDC token for context %q server %s", mcCtx.Name, mcCtx.Server)
		stored = refreshed
	}

	if shouldUseStoredOIDCToken(token, previousStoredToken, stored.Tokens) {
		token = stored.Tokens.AccessToken
		mcCtx.Token = token
		if cfg, err := LoadConfig(); err == nil {
			updateContextToken(cfg, mcCtx.Name, token)
		} else {
			return "", fmt.Errorf("failed to update context token: %w", err)
		}
	}

	return token, nil
}

func shouldRefreshStoredToken(contextToken, previousStoredToken string, stored *storedOIDCToken) bool {
	if stored == nil || stored.Tokens == nil || !oidcTokenExpiring(stored.Tokens) || stored.Tokens.RefreshToken == "" {
		return false
	}
	return contextToken == "" || contextToken == previousStoredToken || !isMissionControlAccessTokenFormat(contextToken)
}
