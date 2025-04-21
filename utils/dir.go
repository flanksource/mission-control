package utils

import (
	"fmt"
	"os"
	"path/filepath"
)

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
