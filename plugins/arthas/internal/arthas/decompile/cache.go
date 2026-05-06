package decompile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cacheDirEnv overrides the user cache root. Used by tests; if unset the
// cache lives under os.UserCacheDir()/mission-control-arthas/decompile.
const cacheDirEnv = "MISSION_CONTROL_ARTHAS_CACHE_DIR"

// cacheRoot returns the directory that holds decompiled-class output. The
// directory is created lazily on Put.
func cacheRoot() (string, error) {
	if v := os.Getenv(cacheDirEnv); v != "" {
		return filepath.Join(v, "decompile"), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("user cache dir: %w", err)
	}
	return filepath.Join(base, "mission-control-arthas", "decompile"), nil
}

// CachePath returns the on-disk file path for a given digest+fqcn pair.
// Exposed for diagnostics; callers normally use Get/Put.
func CachePath(digest, fqcn string) (string, error) {
	root, err := cacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, sanitize(digest), sanitize(fqcn)+".java"), nil
}

// Get returns the cached decompiled source for fqcn under digest, if any.
// Returns ("", false) on cache miss or any I/O error — callers fall back to
// re-fetching via arthas.
func Get(digest, fqcn string) (string, bool) {
	if digest == "" || fqcn == "" {
		return "", false
	}
	path, err := CachePath(digest, fqcn)
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// Put writes the decompiled source for fqcn under digest. Errors are
// returned so callers can log them; a write failure is non-fatal — the next
// run will simply re-fetch from arthas.
func Put(digest, fqcn, source string) error {
	if digest == "" || fqcn == "" {
		return fmt.Errorf("digest and fqcn are required")
	}
	path, err := CachePath(digest, fqcn)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir cache dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		return fmt.Errorf("write cache file: %w", err)
	}
	return nil
}

// sanitize strips path-traversal components from a cache key segment so user-
// influenced names (FQCN, digest) cannot escape the cache root.
func sanitize(s string) string {
	s = strings.ReplaceAll(s, "..", "_")
	s = strings.ReplaceAll(s, string(os.PathSeparator), "_")
	s = strings.ReplaceAll(s, "/", "_")
	return s
}
