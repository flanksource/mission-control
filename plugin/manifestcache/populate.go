package manifestcache

import (
	gocontext "context"
	"errors"
	"fmt"
	"os"
	osExec "os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/clicky/rpc"
	"github.com/flanksource/commons/har"
	goplugin "github.com/hashicorp/go-plugin"

	"github.com/flanksource/incident-commander/pkg/httpobservability"
	"github.com/flanksource/incident-commander/plugin/api"
	"github.com/flanksource/incident-commander/plugin/machinery/local"
	"github.com/flanksource/incident-commander/sdk"
)

// PopulateOptions controls how the cache is filled. Either Server or
// BinaryDir is required (mode selection is the caller's job — see
// cmd/plugin.go).
type PopulateOptions struct {
	// Server / Token select API mode: GET /api/plugins?format=clicky-rpc.
	Server string
	Token  string

	// BinaryDir selects local mode: spawn the binary at
	// filepath.Join(BinaryDir, name) and capture its manifest.
	BinaryDir string

	// StartupTimeout caps the local-mode dial (defaults to 30s).
	StartupTimeout time.Duration

	// HAR is an optional collector that captures the API-mode HTTP traffic
	// (cache refresh hits one endpoint). Local-mode populate ignores it
	// because the gRPC handshake doesn't go through net/http.
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
	written := make([]string, 0, len(services))
	for _, svc := range services {
		if svc.Name == "" {
			continue
		}
		if err := Write(Entry{
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

// PopulateLocal spawns the plugin binary at BinaryDir/name once, captures
// its manifest via RegisterPlugin, writes the sidecar, and shuts the
// plugin down. Returns the entry written.
func PopulateLocal(ctx gocontext.Context, name string, opts PopulateOptions) (*Entry, error) {
	if opts.BinaryDir == "" {
		return nil, errors.New("manifestcache: BinaryDir required")
	}
	binPath, err := findBinary(opts.BinaryDir, name)
	if err != nil {
		return nil, err
	}
	manifest, err := dialAndCaptureManifest(ctx, binPath, opts.StartupTimeout)
	if err != nil {
		return nil, err
	}
	checksum, err := sha256File(binPath)
	if err != nil {
		return nil, fmt.Errorf("manifestcache: hash %s: %w", binPath, err)
	}
	entry := Entry{
		Source:         SourceLocalBinary,
		BinaryPath:     binPath,
		BinaryChecksum: checksum,
		CachedAt:       time.Now(),
		Service:        ManifestToService(manifest),
	}
	if entry.Service.Name == "" {
		entry.Service.Name = name
	}
	if err := Write(entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

func fetchClickyRPCList(ctx gocontext.Context, server, token string) ([]rpc.RPCService, error) {
	services, err := sdk.New(server, token, sdk.WithUserAgent("mission-control-cli/manifestcache")).ListPluginRPCServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("manifestcache: %w", err)
	}
	return services, nil
}

// FindBinaryFor locates the binary for a plugin name on the configured
// MISSION_CONTROL_PLUGIN_PATH. Exported so the CLI dispatch path and the
// cache populate path share the same lookup behaviour.
func FindBinaryFor(name string) (string, error) {
	dir := os.Getenv("MISSION_CONTROL_PLUGIN_PATH")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("MISSION_CONTROL_PLUGIN_PATH unset and home dir unknown: %w", err)
		}
		dir = filepath.Join(home, ".mission-control", "plugins")
	}
	return findBinary(dir, name)
}

// findBinary locates a plugin binary by name within dir. It supports both
// the old flat layout ($dir/name) and the current versioned install layout
// ($dir/name/latest/name or $dir/name/<version>/name).
func findBinary(dir, name string) (string, error) {
	for _, candidate := range []string{
		filepath.Join(dir, name),
		filepath.Join(dir, name, "latest"),
		filepath.Join(dir, name, "latest", name),
		filepath.Join(dir, name, name),
	} {
		if isBinaryFile(candidate) {
			return candidate, nil
		}
	}

	pluginDir := filepath.Join(dir, name)
	if entries, err := os.ReadDir(pluginDir); err == nil {
		for _, e := range entries {
			candidate := filepath.Join(pluginDir, e.Name(), name)
			if isBinaryFile(candidate) {
				return candidate, nil
			}
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("manifestcache: scan %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), name) {
			return filepath.Join(dir, e.Name()), nil
		}
	}
	return "", fmt.Errorf("manifestcache: plugin %q not found in %s", name, dir)
}

func isBinaryFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// dialAndCaptureManifest spawns the plugin, completes RegisterPlugin, and
// returns the manifest. The plugin is killed before this function returns.
func dialAndCaptureManifest(ctx gocontext.Context, binPath string, timeout time.Duration) (*api.PluginManifest, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cmd := osExec.Command(binPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("%s=%s", api.Handshake.MagicCookieKey, api.Handshake.MagicCookieValue),
	)
	cli := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: api.Handshake,
		Plugins: map[string]goplugin.Plugin{
			api.PluginName: &local.GRPCPlugin{},
		},
		Cmd:              cmd,
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Managed:          true,
	})
	defer cli.Kill()

	rpcClient, err := cli.Client()
	if err != nil {
		return nil, fmt.Errorf("manifestcache: rpc client: %w", err)
	}
	raw, err := rpcClient.Dispense(api.PluginName)
	if err != nil {
		return nil, fmt.Errorf("manifestcache: dispense: %w", err)
	}
	pluginCli, ok := raw.(*local.Client)
	if !ok {
		return nil, fmt.Errorf("manifestcache: unexpected dispense type %T", raw)
	}

	dialCtx, cancel := gocontext.WithTimeout(ctx, timeout)
	defer cancel()

	manifest, err := pluginCli.Service.RegisterPlugin(dialCtx, &api.RegisterRequest{
		HostProtocolVersion: uint32(api.ProtocolVersion),
	})
	if err != nil {
		return nil, fmt.Errorf("manifestcache: RegisterPlugin: %w", err)
	}
	return manifest, nil
}
