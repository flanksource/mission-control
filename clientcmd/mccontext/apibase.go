package mccontext

import (
	gocontext "context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OIDCLoginServerCandidates lists the URLs worth attempting a browser login
// against. A server recorded as the frontend's /api path still serves the OIDC
// endpoints at its root, so both are tried, root first.
func OIDCLoginServerCandidates(serverURL string) []string {
	serverURL = strings.TrimRight(serverURL, "/")
	if strings.HasSuffix(serverURL, "/api") {
		return uniqueStrings([]string{strings.TrimSuffix(serverURL, "/api"), serverURL})
	}
	return uniqueStrings([]string{serverURL})
}

// ResolveAPIBase probes both the frontend proxy API path and the direct backend
// path, returning the base URL that serves Mission Control's unauthenticated
// /health endpoint.
func ResolveAPIBase(serverURL string) (string, error) {
	serverURL = strings.TrimRight(serverURL, "/")
	if serverURL == "" {
		return "", fmt.Errorf("server URL is required")
	}

	var failures []string
	for _, candidate := range apiBaseCandidates(serverURL) {
		resolved, ok, err := probeAPIHealth(gocontext.Background(), candidate)
		if err == nil && ok {
			return resolved, nil
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate, err))
		} else {
			failures = append(failures, fmt.Sprintf("%s: /health did not return OK", candidate))
		}
	}
	return "", fmt.Errorf("could not find Mission Control API for %s (%s)", serverURL, strings.Join(failures, "; "))
}

func apiBaseCandidates(serverURL string) []string {
	serverURL = strings.TrimRight(serverURL, "/")
	if strings.HasSuffix(serverURL, "/api") {
		return uniqueStrings([]string{serverURL, strings.TrimSuffix(serverURL, "/api")})
	}
	return uniqueStrings([]string{serverURL + "/api", serverURL})
}

func probeAPIHealth(ctx gocontext.Context, baseURL string) (string, bool, error) {
	healthURL, err := url.JoinPath(baseURL, "health")
	if err != nil {
		return "", false, err
	}
	ctx, cancel := gocontext.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return "", false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(string(body)) != "OK" {
		return "", false, nil
	}

	finalURL := *resp.Request.URL
	if req.URL.Scheme == "https" && finalURL.Scheme != "https" {
		return "", false, fmt.Errorf("health probe redirected from HTTPS to %s", finalURL.Scheme)
	}
	finalURL.RawQuery = ""
	finalURL.Fragment = ""
	finalURL.RawPath = ""
	finalPath := strings.TrimSuffix(finalURL.Path, "/")
	if !strings.HasSuffix(finalPath, "/health") {
		return "", false, fmt.Errorf("health probe redirected to an unexpected path: %s", finalURL.String())
	}
	finalURL.Path = strings.TrimSuffix(finalPath, "/health")
	return strings.TrimRight(finalURL.String(), "/"), true, nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
