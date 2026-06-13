// Package manifestcache stores plugin operation schemas as a JSON sidecar
// file under the user cache dir. It is the source of truth used by the CLI
// to render `mission-control <plugin> --help` without spawning the plugin
// binary on every invocation.
//
// Two writers populate the cache:
//
//   - The host supervisor writes the canonical entry after the plugin
//     completes RegisterPlugin (Source = "local-binary").
//   - The CLI writes an entry after fetching schemas from a remote
//     mission-control over HTTP (Source = "remote-server").
//
// Readers (the CLI) decide staleness from the entry's Source: a local
// entry is invalidated when the binary's sha256 differs; a remote entry
// has a soft TTL refreshed by `mission-control plugin <name> refresh-cache`.
package manifestcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/flanksource/clicky/rpc"
)

// Source enumerates the two ways the cache can be populated.
type Source string

const (
	SourceLocalBinary  Source = "local-binary"
	SourceRemoteServer Source = "remote-server"
)

// Entry is the on-disk envelope for a single plugin's cached schema.
type Entry struct {
	Source         Source         `json:"source"`
	BinaryPath     string         `json:"binary_path,omitempty"`
	BinaryChecksum string         `json:"binary_checksum,omitempty"`
	ServerURL      string         `json:"server_url,omitempty"`
	CachedAt       time.Time      `json:"cached_at"`
	Service        rpc.RPCService `json:"service"`
}

var (
	// ErrMissing is returned by Get when no sidecar exists for a plugin.
	ErrMissing = errors.New("manifestcache: no entry")
	// ErrStale is returned by Get when the cached binary checksum no
	// longer matches the on-disk binary.
	ErrStale = errors.New("manifestcache: entry stale")
	// ErrCorrupt is returned by Get when the sidecar exists but cannot be
	// decoded.
	ErrCorrupt = errors.New("manifestcache: entry corrupt")
)

// dirOverride lets tests redirect the cache to a temp directory.
var dirOverride string

// SetDirForTest overrides the cache directory for the lifetime of the
// returned cleanup function. Test-only; production callers must not use it.
func SetDirForTest(dir string) func() {
	prev := dirOverride
	dirOverride = dir
	return func() { dirOverride = prev }
}

// Dir returns the cache directory path. It does not create it.
func Dir() string {
	if dirOverride != "" {
		return dirOverride
	}
	base, err := os.UserCacheDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "mission-control", "plugins")
}

// Path returns the sidecar path for a given plugin name.
func Path(name string) string {
	return PathInDir(Dir(), name)
}

// PathInDir returns the sidecar path for a plugin name in dir.
func PathInDir(dir, name string) string {
	return filepath.Join(dir, name+".json")
}

// Get reads the sidecar for `name`. For local-binary entries the binary
// at entry.BinaryPath is hashed and compared against entry.BinaryChecksum;
// a mismatch returns ErrStale alongside the (still-readable) entry so the
// caller can decide whether to re-populate.
func Get(name string) (*Entry, error) {
	data, err := os.ReadFile(Path(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrMissing
		}
		return nil, fmt.Errorf("manifestcache: read %s: %w", name, err)
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if e.Source == SourceLocalBinary {
		sum, err := sha256File(e.BinaryPath)
		switch {
		case errors.Is(err, os.ErrNotExist):
			return &e, ErrStale
		case err != nil:
			return &e, fmt.Errorf("manifestcache: hash %s: %w", e.BinaryPath, err)
		case sum != e.BinaryChecksum:
			return &e, ErrStale
		}
	}
	return &e, nil
}

// Write atomically replaces the sidecar for entry.Service.Name. CachedAt
// is set to time.Now if zero.
func Write(entry Entry) error {
	return WriteToDir(Dir(), entry)
}

// WriteToDir atomically replaces the sidecar for entry.Service.Name in dir.
func WriteToDir(dir string, entry Entry) error {
	if entry.Service.Name == "" {
		return errors.New("manifestcache: entry has empty service name")
	}
	if entry.CachedAt.IsZero() {
		entry.CachedAt = time.Now()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("manifestcache: mkdir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("manifestcache: marshal: %w", err)
	}
	final := PathInDir(dir, entry.Service.Name)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("manifestcache: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("manifestcache: rename %s: %w", final, err)
	}
	return nil
}

// Delete removes the sidecar for `name`. Missing files are not an error.
func Delete(name string) error {
	return DeleteFromDir(Dir(), name)
}

// DeleteFromDir removes the sidecar for `name` from dir. Missing files are not an error.
func DeleteFromDir(dir, name string) error {
	if err := os.Remove(PathInDir(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("manifestcache: remove %s: %w", name, err)
	}
	return nil
}

// List returns every cached entry. Corrupt files are skipped silently —
// callers that need strict reads should use Get on a known name.
func List() ([]*Entry, error) {
	return ListFromDir(Dir())
}

// ListFromDir returns every cached entry in dir. Corrupt files are skipped silently.
func ListFromDir(dir string) ([]*Entry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("manifestcache: readdir %s: %w", dir, err)
	}
	out := make([]*Entry, 0, len(entries))
	for _, de := range entries {
		if de.IsDir() || filepath.Ext(de.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, de.Name()))
		if err != nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		out = append(out, &e)
	}
	return out, nil
}

// ClearDir removes cached plugin sidecars from dir without removing the directory itself.
func ClearDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("manifestcache: mkdir %s: %w", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("manifestcache: readdir %s: %w", dir, err)
	}
	for _, de := range entries {
		if de.IsDir() || filepath.Ext(de.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, de.Name())
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("manifestcache: remove %s: %w", path, err)
		}
	}
	return nil
}

// SHA256File hashes a file. Exported for the supervisor and CLI populate
// path; never use it for security-sensitive comparisons (cache keying only).
func SHA256File(path string) (string, error) { return sha256File(path) }

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
