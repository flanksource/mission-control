package utils

import (
	"path/filepath"

	"github.com/flanksource/commons/logger"
)

func UnfoldGlobs(paths ...string) []string {
	unfoldedPaths := make([]string, 0, len(paths))
	for _, path := range paths {
		matched, err := filepath.Glob(path)
		if err != nil {
			logger.Warnf("invalid glob pattern. path=%s; %w", path, err)
			continue
		}

		unfoldedPaths = append(unfoldedPaths, matched...)
	}

	return unfoldedPaths
}
