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

// PersistPluginFromCRD is the kopper persist callback. It installs (or
// re-installs) the plugin's binary, registers the spec, and asks the
// supervisor to (re)start the process.
//
// SupervisorStarter is injected by the cmd/server wiring at boot to break
// the import cycle between registry and supervisor.
var SupervisorStarter func(ctx context.Context, name string) error

func PersistPluginFromCRD(ctx context.Context, p *v1.Plugin) error {
	if p == nil {
		return fmt.Errorf("nil Plugin")
	}
	if p.Spec.Source == "" {
		return fmt.Errorf("plugin %s: spec.source is required", p.Name)
	}

	binDir := PluginPath()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create plugin dir %s: %w", binDir, err)
	}

	binPath := filepath.Join(binDir, p.Name)
	if info, err := os.Stat(binPath); err == nil && !info.IsDir() && p.Spec.Checksum == "" {
		ctx.Logger.V(3).Infof("plugin %s: using existing binary at %s, skipping install", p.Name, binPath)
	} else {
		res, err := deps.InstallWithContext(ctx,
			p.Spec.Source,
			p.Spec.Version,
			deps.WithBinDir(binDir),
		)
		if err != nil {
			return fmt.Errorf("install plugin %s: %w", p.Name, err)
		}
		if res != nil && res.Error != nil {
			return fmt.Errorf("install plugin %s: %w", p.Name, res.Error)
		}
	}

	Default.Upsert(p.Name, p.Spec)

	if SupervisorStarter != nil {
		return SupervisorStarter(ctx, p.Name)
	}
	return nil
}

// SupervisorStopper is injected by the cmd/server wiring (same reason as
// SupervisorStarter).
var SupervisorStopper func(name string) error

// DeletePlugin is the kopper delete callback. It stops the supervised
// process and drops the registry entry. The binary is left on disk —
// re-creating the CRD won't re-download a binary that already exists.
func DeletePlugin(ctx context.Context, id string) error {
	for _, e := range Default.List() {
		if e.Manifest != nil && e.Manifest.Name == id {
			id = e.Manifest.Name
			break
		}
	}
	if SupervisorStopper != nil {
		_ = SupervisorStopper(id)
	}
	Default.Remove(id)
	return nil
}

// DeleteStalePlugin removes registry entries for plugins whose CRD has been
// renamed or replaced.
func DeleteStalePlugin(ctx context.Context, newer *v1.Plugin) error {
	if newer == nil {
		return nil
	}
	for _, e := range Default.List() {
		if e.Manifest != nil && e.Manifest.Name != newer.Name {
			continue
		}
		// Same name and same UID is not stale.
		if e.Spec.Source == newer.Spec.Source && e.Spec.Version == newer.Spec.Version {
			continue
		}
	}
	return nil
}

// BinaryPathFor returns the on-disk path for a plugin's binary. The
// supervisor uses this to exec the process.
func BinaryPathFor(name string) string {
	return filepath.Join(PluginPath(), name)
}
