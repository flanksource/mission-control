package local

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/deps"
	dutyContext "github.com/flanksource/duty/context"
)

// IsLatest reports whether a plugin version pins to a moving "latest" target
// rather than a concrete version.
func IsLatest(version string) bool {
	return version == "" || version == "latest"
}

// binaryName returns the on-disk name used for a plugin's binary and version
// directories. The deps package name (source) is preferred, falling back to
// the plugin name.
func binaryName(name, source string) string {
	if source != "" {
		return source
	}
	return name
}

// VersionFromBinaryPath returns the resolved version a binary was installed
// under, which is the name of the directory containing the binary.
func VersionFromBinaryPath(binPath string) string {
	if binPath == "" {
		return ""
	}
	return filepath.Base(filepath.Dir(binPath))
}

// RemoveVersion deletes the version directory for one plugin version. It
// refuses to delete the moving "latest" target so a half-resolved install
// can't wipe an unrelated directory.
func RemoveVersion(name, source, version string) error {
	if IsLatest(version) {
		return fmt.Errorf("refusing to remove plugin %s version dir for %q", name, version)
	}
	return os.RemoveAll(VersionedBinDirFor(binaryName(name, source), version))
}

func InstallPlugin(ctx dutyContext.Context, name, source, version string) (string, error) {
	binDir := PluginPath()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("create plugin dir %s: %w", binDir, err)
	}

	binName := binaryName(name, source)
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

// ResolveAndInstallLatest re-resolves a plugin's "latest" target to a concrete
// version and pins it under that version's directory. It returns the installed
// binary path and the resolved version so callers can detect when a newer
// version has become available.
//
// The resolution installs "latest" into a persistent staging directory. deps
// always reports the resolved concrete version, and only re-downloads when the
// staged binary is behind the upstream latest, so repeated calls are cheap when
// nothing has changed. The staged binary is then copied into its versioned
// directory, leaving staging warm for the next check.
func ResolveAndInstallLatest(ctx dutyContext.Context, name, source string) (string, string, error) {
	binName := binaryName(name, source)
	staging := VersionedBinDirFor(binName, "latest")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return "", "", fmt.Errorf("create plugin staging dir %s: %w", staging, err)
	}

	res, err := deps.InstallWithContext(ctx, source, "latest", deps.WithBinDir(staging))
	if err != nil {
		return "", "", fmt.Errorf("resolve latest for plugin %s: %w", name, err)
	}
	if res != nil && res.Error != nil {
		return "", "", fmt.Errorf("resolve latest for plugin %s: %w", name, res.Error)
	}

	version := strings.TrimSpace(res.Version.String())
	if IsLatest(version) {
		return "", "", fmt.Errorf("plugin %s: deps did not resolve latest to a concrete version", name)
	}

	target, err := pinVersion(binName, version)
	if err != nil {
		return "", "", err
	}

	return target, version, nil
}

// pinVersion copies the freshly staged binary into its versioned directory and
// returns the binary's path. When that version is already pinned the existing
// binary is reused and the staged copy is left untouched.
func pinVersion(binName, version string) (string, error) {
	target := BinaryPathFor(binName, version)
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		return target, nil
	}

	if err := os.MkdirAll(VersionedBinDirFor(binName, version), 0o755); err != nil {
		return "", fmt.Errorf("create plugin version dir for %s@%s: %w", binName, version, err)
	}
	if err := copyExecutable(BinaryPathFor(binName, "latest"), target); err != nil {
		return "", fmt.Errorf("pin plugin %s@%s: %w", binName, version, err)
	}

	return target, nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
