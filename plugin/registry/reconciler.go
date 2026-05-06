package registry

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/flanksource/deps"
	"github.com/flanksource/duty/context"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// EnvPluginPath is the env var that controls where plugin binaries are
// installed and discovered.
const EnvPluginPath = "MISSION_CONTROL_PLUGIN_PATH"

// PluginPath returns the directory where plugin binaries live. Defaults to
// $HOME/.mission-control/plugins when the env var is unset.
func PluginPath() string {
	if v := os.Getenv(EnvPluginPath); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".mission-control-plugins"
	}
	return filepath.Join(home, ".mission-control", "plugins")
}

// SupervisorStarter is injected by the cmd/server wiring at boot to break
// the import cycle between registry and supervisor.
var SupervisorStarter func(ctx context.Context, name string) error

// SupervisorStopper is injected by the cmd/server wiring (same reason as
// SupervisorStarter).
var SupervisorStopper func(name string) error

// ApplyToRegistry installs (or re-installs) the binary, upserts the registry
// entry, and asks the supervisor to (re)start. Called by the DB-side kopper
// callbacks after persistence succeeds, and by the cold-start replay that
// rehydrates the registry from the plugins table on boot.
func ApplyToRegistry(ctx context.Context, name string, spec v1.PluginSpec) (string, error) {
	if name == "" {
		return "", fmt.Errorf("plugin name is required")
	}
	if spec.Source == "" {
		return "", fmt.Errorf("plugin %s: spec.source is required", name)
	}

	binDir := PluginPath()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("create plugin dir %s: %w", binDir, err)
	}

	binPath := filepath.Join(binDir, name)
	if info, err := os.Stat(binPath); err == nil && !info.IsDir() && spec.Checksum == "" {
		ctx.Logger.V(3).Infof("plugin %s: using existing binary at %s, skipping install", name, binPath)
	} else {
		res, err := deps.InstallWithContext(ctx,
			spec.Source,
			spec.Version,
			deps.WithBinDir(binDir),
		)
		if err != nil {
			return "", fmt.Errorf("install plugin %s: %w", name, err)
		}
		if res != nil && res.Error != nil {
			return "", fmt.Errorf("install plugin %s: %w", name, res.Error)
		}
	}

	Default.Upsert(name, spec)

	if SupervisorStarter != nil {
		if err := SupervisorStarter(ctx, name); err != nil {
			return binPath, err
		}
	}
	return binPath, nil
}

// RemoveFromRegistry stops the supervised process and drops the registry
// entry for the given plugin name. The binary is left on disk.
func RemoveFromRegistry(name string) {
	if name == "" {
		return
	}
	if SupervisorStopper != nil {
		_ = SupervisorStopper(name)
	}
	Default.Remove(name)
}

// StaleFromRegistry drops registry entries whose Plugin CRD has been
// renamed or replaced. Currently a no-op when there is nothing to clean —
// kept as a parity point with DeleteStale callbacks for other CRDs.
func StaleFromRegistry(newer *v1.Plugin) {
	if newer == nil {
		return
	}
	for _, e := range Default.List() {
		if e.Manifest == nil || e.Manifest.Name == newer.Name {
			continue
		}
		if e.Spec.Source == newer.Spec.Source && e.Spec.Version == newer.Spec.Version {
			continue
		}
	}
}

// BinaryPathFor returns the on-disk path for a plugin's binary. The
// supervisor uses this to exec the process.
func BinaryPathFor(name string) string {
	return filepath.Join(PluginPath(), name)
}
