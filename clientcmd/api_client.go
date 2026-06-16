package clientcmd

import (
	"context"
	"fmt"
	"sync"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/auth/oidcclient"
	"github.com/flanksource/incident-commander/sdk"
)

func NewAPIClient(mcCtx *MCContext, opts ...sdk.ClientOption) *sdk.Client {
	if mcCtx == nil {
		return nil
	}
	opts = append([]sdk.ClientOption{sdk.WithTokenProvider(contextTokenProvider(mcCtx))}, opts...)
	return NewAPIClientForServer(mcCtx.Server, mcCtx.AccessToken(), opts...)
}

func NewAPIClientForServer(server, token string, opts ...sdk.ClientOption) *sdk.Client {
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
	if mcCtx.Server == "" || mcCtx.OIDC == nil {
		return mcCtx.AccessToken(), nil
	}
	if !shouldRefreshOIDCToken(mcCtx.OIDC) {
		return mcCtx.AccessToken(), nil
	}

	previousAccessToken := mcCtx.OIDC.AccessToken
	logger.Debugf("refreshing OIDC token for context %q server %s", mcCtx.Name, mcCtx.Server)
	refreshed, err := refreshOIDCTokens(mcCtx.Server, mcCtx.OIDC)
	if err != nil {
		logger.Debugf("failed to refresh OIDC token for context %q server %s: %v", mcCtx.Name, mcCtx.Server, err)
		if mcCtx.Token != "" && mcCtx.Token != previousAccessToken {
			return mcCtx.Token, nil
		}
		return "", fmt.Errorf("refresh OIDC token for %s: %w", mcCtx.Server, err)
	}

	logger.Debugf("refreshed OIDC token for context %q server %s", mcCtx.Name, mcCtx.Server)
	mcCtx.SetOIDCTokens(refreshed)
	if cfg, err := LoadConfig(); err == nil {
		updateContextOIDCTokens(cfg, mcCtx.Name, refreshed)
	} else {
		return "", fmt.Errorf("failed to update context OIDC tokens: %w", err)
	}
	return mcCtx.AccessToken(), nil
}

func shouldRefreshOIDCToken(tokens *oidcclient.Tokens) bool {
	return tokens != nil && oidcTokenExpiring(tokens) && tokens.RefreshToken != ""
}
