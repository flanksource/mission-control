// Package localcache populates the plugin manifest cache from local binaries.
// It owns the go-plugin and gRPC dependencies that remote clients do not need.
package localcache

import (
	gocontext "context"
	"errors"
	"fmt"
	"os"
	osExec "os/exec"
	"path/filepath"
	"strings"
	"time"

	goplugin "github.com/hashicorp/go-plugin"

	"github.com/flanksource/incident-commander/plugin/api"
	pluginlocal "github.com/flanksource/incident-commander/plugin/machinery/local"
	"github.com/flanksource/incident-commander/plugin/manifestadapter"
	"github.com/flanksource/incident-commander/plugin/manifestcache"
)

// PopulateOptions controls local plugin discovery and cache storage.
type PopulateOptions struct {
	BinaryDir      string
	StartupTimeout time.Duration
	CacheDir       string
}

// Populate captures a local plugin's manifest and writes its cache entry.
func Populate(ctx gocontext.Context, name string, opts PopulateOptions) (*manifestcache.Entry, error) {
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
	checksum, err := manifestcache.SHA256File(binPath)
	if err != nil {
		return nil, fmt.Errorf("manifestcache: hash %s: %w", binPath, err)
	}
	entry := manifestcache.Entry{
		Source:         manifestcache.SourceLocalBinary,
		BinaryPath:     binPath,
		BinaryChecksum: checksum,
		CachedAt:       time.Now(),
		Service:        manifestadapter.ManifestToService(manifest),
	}
	if entry.Service.Name == "" {
		entry.Service.Name = name
	}
	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir = manifestcache.Dir()
	}
	if err := manifestcache.WriteToDir(cacheDir, entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// FindBinaryFor locates a plugin binary on MISSION_CONTROL_PLUGIN_PATH.
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
		for _, entry := range entries {
			candidate := filepath.Join(pluginDir, entry.Name(), name)
			if isBinaryFile(candidate) {
				return candidate, nil
			}
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("manifestcache: scan %s: %w", dir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), name) {
			return filepath.Join(dir, entry.Name()), nil
		}
	}
	return "", fmt.Errorf("manifestcache: plugin %q not found in %s", name, dir)
}

func isBinaryFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dialAndCaptureManifest(ctx gocontext.Context, binPath string, timeout time.Duration) (*api.PluginManifest, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cmd := osExec.Command(binPath)
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%s", api.Handshake.MagicCookieKey, api.Handshake.MagicCookieValue))
	cli := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: api.Handshake,
		Plugins: map[string]goplugin.Plugin{
			api.PluginName: &pluginlocal.GRPCPlugin{},
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
	pluginClient, ok := raw.(*pluginlocal.Client)
	if !ok {
		return nil, fmt.Errorf("manifestcache: unexpected dispense type %T", raw)
	}

	dialCtx, cancel := gocontext.WithTimeout(ctx, timeout)
	defer cancel()
	manifest, err := pluginClient.Service.RegisterPlugin(dialCtx, &api.RegisterRequest{
		HostProtocolVersion: uint32(api.ProtocolVersion),
	})
	if err != nil {
		return nil, fmt.Errorf("manifestcache: RegisterPlugin: %w", err)
	}
	return manifest, nil
}
