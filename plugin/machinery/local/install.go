package local

import (
	"fmt"
	"os"

	"github.com/flanksource/deps"
	dutyContext "github.com/flanksource/duty/context"
)

func InstallPlugin(ctx dutyContext.Context, name, source, version string) (string, error) {
	binDir := PluginPath()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("create plugin dir %s: %w", binDir, err)
	}

	binName := source
	if binName == "" {
		binName = name
	}
	versionedBinDir := VersionedBinDirFor(binName, version)
	if err := os.MkdirAll(versionedBinDir, 0o755); err != nil {
		return "", fmt.Errorf("create plugin version dir %s: %w", versionedBinDir, err)
	}

	binPath := BinaryPathFor(binName, version)
	if info, err := os.Stat(binPath); err == nil && !info.IsDir() {
		ctx.Logger.V(3).Infof("plugin %s@%s: using existing binary at %s, skipping install", name, version, binPath)
		return binPath, nil
	}

	res, err := deps.InstallWithContext(ctx, source, version, deps.WithBinDir(versionedBinDir))
	if err != nil {
		return "", fmt.Errorf("install plugin %s: %w", name, err)
	}
	if res != nil && res.Error != nil {
		return "", fmt.Errorf("install plugin %s: %w", name, res.Error)
	}

	if info, err := os.Stat(binPath); err != nil {
		return "", fmt.Errorf("installed plugin binary %s not found: %w", binPath, err)
	} else if info.IsDir() {
		return "", fmt.Errorf("installed plugin binary %s is a directory", binPath)
	}

	return binPath, nil
}
