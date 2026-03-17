package utils

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

var (
	maxAgeRegex         = regexp.MustCompile(`max-age=([^,\s]+)`)
	refreshTimeoutRegex = regexp.MustCompile(`refresh-timeout=([^,\s]+)`)
)

func HTTPFileserver(embeddedFS embed.FS) (http.Handler, error) {
	fsys, err := fs.Sub(embeddedFS, ".")
	if err != nil {
		return nil, err
	}
	return http.FileServer(http.FS(fsys)), nil
}

// ParseCacheControlHeader parses Cache-Control header for max-age and refresh-timeout
func ParseCacheControlHeader(cacheControl string) (maxAge, refreshTimeout time.Duration, err error) {
	if cacheControl == "" {
		return 0, 0, nil
	}

	maxAge, err = parseDirective(cacheControl, maxAgeRegex)
	if err != nil {
		return 0, 0, err
	}

	refreshTimeout, err = parseDirective(cacheControl, refreshTimeoutRegex)
	if err != nil {
		return 0, 0, err
	}

	return maxAge, refreshTimeout, nil
}

func parseDirective(cacheControl string, regex *regexp.Regexp) (time.Duration, error) {
	if matches := regex.FindStringSubmatch(cacheControl); len(matches) > 1 {
		seconds, err := strconv.Atoi(matches[1])
		if err != nil {
			return 0, fmt.Errorf("invalid directive value: %s", matches[1])
		}
		if seconds < 0 {
			return 0, fmt.Errorf("invalid directive value: %s (negative values not allowed)", matches[1])
		}
		return time.Duration(seconds) * time.Second, nil
	}

	return 0, nil
}
