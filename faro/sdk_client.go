package main

import (
	"fmt"

	"github.com/flanksource/incident-commander/clientcmd/mccontext"
	"github.com/flanksource/incident-commander/sdk"
)

func fullRemoteClient() (*sdk.Client, error) {
	cfg, err := mccontext.LoadConfig()
	if err != nil {
		return nil, err
	}
	mcCtx := cfg.CurrentMCContext()
	if mcCtx == nil || mcCtx.Server == "" {
		return nil, fmt.Errorf("no Mission Control server context configured; run `auth login --server <url>` or `context add --server <url> --use`")
	}
	return sdk.New(
		mcCtx.Server,
		mcCtx.AccessToken(),
		sdk.WithTokenProvider(mccontext.ContextTokenProvider(mcCtx)),
		sdk.WithRetry(mccontext.RetryAttempts, mccontext.RetryDelay),
	), nil
}
