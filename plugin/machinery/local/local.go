package local

import (
	"os"
	"path/filepath"
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

// VersionedBinDirFor returns the install directory for one plugin version.
func VersionedBinDirFor(name, version string) string {
	if version == "" {
		version = "latest"
	}
	return filepath.Join(PluginPath(), name, version)
}

// BinaryPathFor returns the on-disk path for a plugin's binary. The
// supervisor uses this to exec the process.
func BinaryPathFor(name, version string) string {
	return filepath.Join(VersionedBinDirFor(name, version), name)
}
