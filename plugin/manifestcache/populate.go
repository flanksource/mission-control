package manifestcache

import (
	gocontext "context"
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/commons/har"

	"github.com/flanksource/incident-commander/clientapi"
	"github.com/flanksource/incident-commander/pkg/httpobservability"
	sdk "github.com/flanksource/incident-commander/sdk/client"
)

// PopulateOptions controls how the cache is filled from Mission Control.
type PopulateOptions struct {
	// Server and Token select the Mission Control API.
	Server string
	Token  string

	// CacheDir overrides the default manifest cache directory.
	CacheDir string

	// ClearExisting removes existing cached sidecars in CacheDir before writing
	// freshly fetched services. The clear happens only after a successful fetch.
	ClearExisting bool

	// HAR is an optional collector that captures the cache refresh request.
	HAR *har.Collector
}

// PopulateAPI fetches schemas from the configured server and writes one
// sidecar entry per plugin returned. Returns the names of plugins written.
func PopulateAPI(ctx gocontext.Context, opts PopulateOptions) ([]string, error) {
	if opts.Server == "" {
		return nil, errors.New("manifestcache: server URL required")
	}
	restore := func() {}
	if opts.HAR != nil {
		restore = httpobservability.SetHARCollector(opts.HAR)
	}
	defer restore()

	services, err := fetchClickyRPCList(ctx, opts.Server, opts.Token)
	if err != nil {
		return nil, err
	}
	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir = Dir()
	}
	if opts.ClearExisting {
		if err := ClearDir(cacheDir); err != nil {
			return nil, err
		}
	}

	written := make([]string, 0, len(services))
	for _, svc := range services {
		if svc.Name == "" {
			continue
		}
		if err := WriteToDir(cacheDir, Entry{
			Source:    SourceRemoteServer,
			ServerURL: opts.Server,
			CachedAt:  time.Now(),
			Service:   svc,
		}); err != nil {
			return written, err
		}
		written = append(written, svc.Name)
	}
	return written, nil
}

func fetchClickyRPCList(ctx gocontext.Context, server, token string) ([]clientapi.PluginService, error) {
	services, err := sdk.New(server, token, sdk.WithUserAgent("mission-control-cli/manifestcache")).ListPluginRPCServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("manifestcache: %w", err)
	}
	return services, nil
}
