package mccontext

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/auth/oidcclient"
	"github.com/flanksource/incident-commander/clientcmd/credentials"
	sdk "github.com/flanksource/incident-commander/sdk/client"
)

// RetryAttempts and RetryDelay are the client's transient-failure policy.
// Defaults live here rather than only in the CLI's flag registration because
// the plugin cache is populated before cobra parses anything, and it would
// otherwise read a zero policy.
var (
	RetryAttempts = 3
	RetryDelay    = time.Second
)

// RetryOption is the one place the policy becomes a client option.
func RetryOption() sdk.ClientOption {
	return sdk.WithRetry(RetryAttempts, RetryDelay)
}

// RemoteClient returns an SDK client bound to the current Mission Control
// context. The returned client's token provider resolves and refreshes the
// stored token per request, so callers without a cobra command (e.g. clicky
// entity handlers) can use it directly. Errors when no server context is set.
func RemoteClient() (*sdk.Client, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	mcCtx := cfg.CurrentMCContext()
	if mcCtx == nil || mcCtx.Server == "" {
		return nil, fmt.Errorf("no Mission Control server context configured; run `auth login --server <url>` or `context add --server <url> --use`")
	}
	return NewAPIClient(mcCtx), nil
}

func NewAPIClient(mcCtx *MCContext, opts ...sdk.ClientOption) *sdk.Client {
	if mcCtx == nil {
		return nil
	}
	opts = append([]sdk.ClientOption{sdk.WithTokenProvider(ContextTokenProvider(mcCtx))}, opts...)
	return NewAPIClientForServer(mcCtx.Server, mcCtx.AccessToken(), opts...)
}

// Retry is applied here rather than at each command's client so every context-bound client shares
// one policy. It is prepended so an explicit caller option still wins, and it is safe to apply
// this broadly because the policy itself decides what may be replayed: a client that only ever
// writes never retries, whatever the flag says.
func NewAPIClientForServer(server, token string, opts ...sdk.ClientOption) *sdk.Client {
	return sdk.New(server, token, append([]sdk.ClientOption{RetryOption()}, opts...)...)
}

func ContextTokenProvider(mcCtx *MCContext) func(context.Context) (string, error) {
	var mu sync.Mutex
	return func(context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return ResolveContextToken(mcCtx)
	}
}

// ResolveContextToken returns a usable access token for the context, refreshing
// an expiring OIDC token first. Exported because the login flow checks whether
// a stored refresh token still works before starting a browser login.
func ResolveContextToken(mcCtx *MCContext) (string, error) {
	if mcCtx == nil {
		return "", nil
	}
	if mcCtx.NeedsReauth != "" {
		return "", mcCtx.ReauthError()
	}
	if mcCtx.Server == "" || !shouldRefreshOIDCToken(mcCtx.OIDC) {
		return mcCtx.AccessToken(), nil
	}
	if err := RefreshContextToken(mcCtx); err != nil {
		return "", err
	}
	return mcCtx.AccessToken(), nil
}

// RefreshContextToken exchanges the refresh token and persists the rotated one.
//
// The server rotates refresh tokens single-use with no grace window, so the
// whole exchange runs under a cross-process lock and re-reads the credential
// inside it: a token spent by a concurrent process must never be spent again,
// and a token this process spends must be persisted or loudly reported.
func RefreshContextToken(mcCtx *MCContext) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	store, err := cfg.store()
	if err != nil {
		return err
	}

	return credentials.WithLock(configDir(), func() error {
		cred, err := store.Get(mcCtx.Name)
		if err != nil {
			return err
		}
		if cred.IsZero() {
			cred = mcCtx.credential()
		}
		mcCtx.applyCredential(cred)

		if cred.NeedsReauth != "" {
			return mcCtx.ReauthError()
		}
		if cred.OIDC == nil || cred.OIDC.RefreshToken == "" {
			mcCtx.NeedsReauth = "no refresh token"
			return mcCtx.ReauthError()
		}
		if !OIDCTokenExpiring(cred.OIDC) {
			logger.Debugf("context %q was refreshed by another process while waiting for the lock", mcCtx.Name)
			return nil
		}

		// Verify the rotated token can be stored BEFORE spending the current
		// one. A read-only config directory is what silently destroyed
		// credentials before the store existed.
		if err := store.Writable(); err != nil {
			return fmt.Errorf("refusing to rotate the refresh token for context %q: credential store (%s) is not writable: %w",
				mcCtx.Name, store.Name(), err)
		}

		tokenEndpoint, err := contextTokenEndpoint(cfg, mcCtx)
		if err != nil {
			return err
		}

		logger.Debugf("refreshing OIDC token for context %q via %s", mcCtx.Name, tokenEndpoint)
		refreshed, err := oidcclient.RefreshToken(tokenEndpoint, cred.OIDC.RefreshToken)
		if err != nil {
			// Any other error leaves the outcome unknown — the server may have
			// rotated without us seeing the response — so the stored token is
			// left untouched and never retried.
			var tokenErr *oidcclient.TokenError
			if !errors.As(err, &tokenErr) || !tokenErr.Terminal() {
				return fmt.Errorf("refresh OIDC token for %s: %w", mcCtx.Server, err)
			}

			cred.OIDC = nil
			cred.NeedsReauth = "refresh token rejected"
			mcCtx.applyCredential(cred)
			if setErr := store.Set(mcCtx.Name, cred); setErr != nil {
				logger.Debugf("failed to record that context %q needs re-authentication: %v", mcCtx.Name, setErr)
			}
			return mcCtx.ReauthError()
		}

		carryForwardTokens(refreshed, cred.OIDC)
		cred.OIDC = refreshed
		if err := store.Set(mcCtx.Name, cred); err != nil {
			return fmt.Errorf("the refresh token for context %q was rotated but could not be saved to the %s store, "+
				"so it is now lost: %w", mcCtx.Name, store.Name(), err)
		}
		mcCtx.applyCredential(cred)
		return nil
	})
}

// carryForwardTokens keeps fields the server omitted from the refresh response.
func carryForwardTokens(refreshed, previous *oidcclient.Tokens) {
	if refreshed == nil || previous == nil {
		return
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = previous.RefreshToken
	}
	if refreshed.IDToken == "" {
		refreshed.IDToken = previous.IDToken
	}
}

func shouldRefreshOIDCToken(tokens *oidcclient.Tokens) bool {
	return tokens != nil && OIDCTokenExpiring(tokens) && tokens.RefreshToken != ""
}
