package clientcmd

import (
	"fmt"

	"github.com/flanksource/incident-commander/sdk"
)

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
