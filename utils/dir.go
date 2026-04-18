package utils

import (
	"fmt"
	"os"
	"path/filepath"
)

// SafeJoin joins pathPrefix with name and verifies the final path remains within pathPrefix.
// This prevents path traversal when name is derived from user-controlled input.
func SafeJoin(pathPrefix, name string) (string, error) {
	baseAbs, err := filepath.Abs(filepath.Clean(pathPrefix))
	if err != nil {
		return "", fmt.Errorf("failed to resolve base path: %w", err)
	}

	targetAbs, err := filepath.Abs(filepath.Join(baseAbs, name))
	if err != nil {
		return "", fmt.Errorf("failed to resolve target path: %w", err)
	}

	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return "", fmt.Errorf("failed to verify target path: %w", err)
	}
	if rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return "", fmt.Errorf("invalid path: outside base directory")
	}

	return targetAbs, nil
}

// CreateTempSubdir creates a temporary directory in the current working directory.
//
// It's helpful when we need to create a temp dir in a relative path
// as it returns the absolute path to the temp dir.
func CreateTempSubdir(base string, pattern string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}

	baseDir := filepath.Join(wd, base)
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create %s directory: %w", base, err)
	}

	dir, err := os.MkdirTemp(baseDir, pattern)
	if err != nil {
		return "", fmt.Errorf("failed to create temporary directory: %w", err)
	}

	return dir, nil
}
