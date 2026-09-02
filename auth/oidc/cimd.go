package oidc

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

const (
	maxClientIDMetadataSize    = 5 << 10
	maxClientIDMetadataCache   = 128
	defaultClientMetadataTTL   = 5 * time.Minute
	maxClientMetadataTTL       = time.Hour
	clientMetadataFetchTimeout = 5 * time.Second
)

type clientIDMetadataDocument struct {
	ClientID                string          `json:"client_id"`
	ClientName              string          `json:"client_name"`
	RedirectURIs            []string        `json:"redirect_uris"`
	GrantTypes              []string        `json:"grant_types"`
	ResponseTypes           []string        `json:"response_types"`
	TokenEndpointAuthMethod string          `json:"token_endpoint_auth_method"`
	ClientSecret            json.RawMessage `json:"client_secret"`
	ClientSecretExpiresAt   json.RawMessage `json:"client_secret_expires_at"`
}

type cachedClientMetadata struct {
	metadata  clientMetadata
	expiresAt time.Time
}

// clientIDMetadataResolver fetches untrusted CIMD documents through an
// SSRF-resistant transport and retains only valid documents in a bounded cache.
type clientIDMetadataResolver struct {
	client *http.Client
	cache  *lru.Cache[string, cachedClientMetadata]
}

func newClientIDMetadataResolver() *clientIDMetadataResolver {
	metadataCache, err := lru.New[string, cachedClientMetadata](maxClientIDMetadataCache)
	if err != nil {
		panic(fmt.Sprintf("create client metadata cache: %v", err))
	}

	transport := &http.Transport{
		DialContext:            dialClientMetadata,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           16,
		MaxIdleConnsPerHost:    2,
		MaxConnsPerHost:        4,
		IdleConnTimeout:        30 * time.Second,
		ResponseHeaderTimeout:  3 * time.Second,
		TLSHandshakeTimeout:    3 * time.Second,
		ExpectContinueTimeout:  1 * time.Second,
		MaxResponseHeaderBytes: 16 << 10,
	}

	return &clientIDMetadataResolver{
		client: &http.Client{
			Transport: transport,
			Timeout:   clientMetadataFetchTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		cache: metadataCache,
	}
}

func (r *clientIDMetadataResolver) resolve(ctx gocontext.Context, clientID string) (*clientMetadata, error) {
	if cached, ok := r.cache.Get(clientID); ok {
		if time.Now().Before(cached.expiresAt) {
			metadata := cached.metadata
			metadata.RedirectURIs = append([]string(nil), metadata.RedirectURIs...)
			return &metadata, nil
		}
		r.cache.Remove(clientID)
	}

	metadata, ttl, err := r.fetch(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if ttl > 0 {
		cachedMetadata := *metadata
		cachedMetadata.RedirectURIs = append([]string(nil), metadata.RedirectURIs...)
		r.cache.Add(clientID, cachedClientMetadata{metadata: cachedMetadata, expiresAt: time.Now().Add(ttl)})
	}
	return metadata, nil
}

func (r *clientIDMetadataResolver) fetch(ctx gocontext.Context, clientID string) (*clientMetadata, time.Duration, error) {
	if _, err := parseClientIDMetadataURL(clientID); err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create client metadata request: %w", err)
	}
	req.Header.Set("Accept", "application/json, application/*+json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch client metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("client metadata returned HTTP %d", resp.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	isJSON := mediaType == "application/json" ||
		strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json")
	if err != nil || !isJSON {
		return nil, 0, fmt.Errorf("client metadata response is not JSON")
	}
	if resp.ContentLength > maxClientIDMetadataSize {
		return nil, 0, fmt.Errorf("client metadata exceeds %d bytes", maxClientIDMetadataSize)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxClientIDMetadataSize+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read client metadata: %w", err)
	}
	if len(body) > maxClientIDMetadataSize {
		return nil, 0, fmt.Errorf("client metadata exceeds %d bytes", maxClientIDMetadataSize)
	}

	var document clientIDMetadataDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, 0, fmt.Errorf("client metadata is not valid JSON: %w", err)
	}
	metadata, err := validateClientIDMetadataDocument(clientID, document)
	if err != nil {
		return nil, 0, err
	}

	return metadata, clientIDMetadataCacheTTL(resp.Header, time.Now()), nil
}

func validateClientIDMetadataDocument(clientID string, document clientIDMetadataDocument) (*clientMetadata, error) {
	if document.ClientID == "" || document.ClientID != clientID {
		return nil, fmt.Errorf("client metadata client_id must exactly match its document URL")
	}

	name := strings.TrimSpace(document.ClientName)
	if name == "" {
		return nil, fmt.Errorf("client metadata client_name is required")
	}
	redirectURIs, err := validateRedirectURIs(document.RedirectURIs)
	if err != nil {
		return nil, fmt.Errorf("client metadata: %w", err)
	}

	if method := document.TokenEndpointAuthMethod; method != "" && method != string(oidc.AuthMethodNone) {
		return nil, fmt.Errorf("client metadata must describe a public client")
	}
	if len(document.ClientSecret) > 0 || len(document.ClientSecretExpiresAt) > 0 {
		return nil, fmt.Errorf("client metadata must not contain a client secret")
	}

	grantTypes := []oidc.GrantType{oidc.GrantTypeCode}
	if len(document.GrantTypes) > 0 {
		grantTypes = make([]oidc.GrantType, 0, len(document.GrantTypes))
	}
	hasAuthorizationCode := false
	for _, grantType := range document.GrantTypes {
		switch grantType {
		case string(oidc.GrantTypeCode):
			hasAuthorizationCode = true
		case string(oidc.GrantTypeRefreshToken):
		default:
			return nil, fmt.Errorf("client metadata contains unsupported grant_type %q", grantType)
		}
		grantTypes = append(grantTypes, oidc.GrantType(grantType))
	}
	if len(document.GrantTypes) > 0 && !hasAuthorizationCode {
		return nil, fmt.Errorf("client metadata must support the authorization_code grant")
	}

	hasCodeResponse := len(document.ResponseTypes) == 0
	for _, responseType := range document.ResponseTypes {
		if responseType != string(oidc.ResponseTypeCode) {
			return nil, fmt.Errorf("client metadata contains unsupported response_type %q", responseType)
		}
		hasCodeResponse = true
	}
	if !hasCodeResponse {
		return nil, fmt.Errorf("client metadata must support the code response type")
	}

	return &clientMetadata{Name: name, RedirectURIs: redirectURIs, GrantTypes: grantTypes}, nil
}

func isClientIDMetadataDocument(clientID string) bool {
	_, err := parseClientIDMetadataURL(clientID)
	return err == nil
}

// parseClientIDMetadataURL applies URL-level CIMD and SSRF checks. DNS results
// are checked and pinned later by dialClientMetadata.
func parseClientIDMetadataURL(rawURL string) (*url.URL, error) {
	if len(rawURL) > maxClientIDLength {
		return nil, fmt.Errorf("client_id exceeds %d characters", maxClientIDLength)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("client_id is not a valid URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.Path == "" || parsed.Opaque != "" {
		return nil, fmt.Errorf("client_id must be an HTTPS URL with a path")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("client_id must not contain a username or password")
	}
	if parsed.Fragment != "" || strings.Contains(rawURL, "#") {
		return nil, fmt.Errorf("client_id must not contain a fragment")
	}
	if parsed.Hostname() == "" || isSpecialMetadataHostname(parsed.Hostname()) {
		return nil, fmt.Errorf("client_id must use a public hostname")
	}
	if literal, err := netip.ParseAddr(parsed.Hostname()); err == nil && !isPublicMetadataAddress(literal) {
		return nil, fmt.Errorf("client_id must use a public IP address")
	}

	for _, segment := range strings.Split(parsed.EscapedPath(), "/") {
		segment, err = url.PathUnescape(segment)
		if err != nil {
			return nil, fmt.Errorf("client_id contains an invalid path")
		}
		if segment == "." || segment == ".." {
			return nil, fmt.Errorf("client_id must not contain dot path segments")
		}
	}

	return parsed, nil
}

func isSpecialMetadataHostname(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	for _, suffix := range []string{"localhost", "local", "home.arpa", "internal"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// dialClientMetadata prevents DNS rebinding by validating every resolved
// address and connecting to a validated IP literal rather than resolving again.
func dialClientMetadata(ctx gocontext.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid client metadata address: %w", err)
	}

	var addresses []netip.Addr
	if literal, err := netip.ParseAddr(host); err == nil {
		addresses = []netip.Addr{literal}
	} else {
		addresses, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve client metadata host: %w", err)
		}
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("client metadata host has no IP addresses")
	}

	for _, address := range addresses {
		if !isPublicMetadataAddress(address) {
			return nil, fmt.Errorf("client metadata host resolves to a non-public address")
		}
	}

	dialer := net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for i, ip := range addresses {
		if i == 8 {
			break
		}
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return connection, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("connect to client metadata host: %w", lastErr)
}

var blockedClientMetadataPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

func isPublicMetadataAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() {
		return false
	}
	for _, blocked := range blockedClientMetadataPrefixes {
		if blocked.Contains(address) {
			return false
		}
	}
	return true
}

// clientIDMetadataCacheTTL honors explicit freshness while imposing an upper
// bound; responses without cache policy receive a short local default.
func clientIDMetadataCacheTTL(header http.Header, now time.Time) time.Duration {
	var maxAge, sharedMaxAge *time.Duration
	for _, value := range header.Values("Cache-Control") {
		for _, directive := range strings.Split(value, ",") {
			name, rawValue, _ := strings.Cut(strings.TrimSpace(directive), "=")
			switch strings.ToLower(name) {
			case "no-cache", "no-store":
				return 0
			case "max-age", "s-maxage":
				seconds, err := strconv.ParseInt(strings.Trim(rawValue, `"`), 10, 64)
				if err != nil || seconds < 0 {
					return 0
				}
				ttl := time.Duration(seconds) * time.Second
				if strings.EqualFold(name, "s-maxage") {
					sharedMaxAge = &ttl
				} else {
					maxAge = &ttl
				}
			}
		}
	}

	selectedMaxAge := sharedMaxAge
	if selectedMaxAge == nil {
		selectedMaxAge = maxAge
	}
	if selectedMaxAge != nil {
		age := time.Duration(0)
		if date, err := http.ParseTime(header.Get("Date")); err == nil && now.After(date) {
			age = now.Sub(date)
		}
		if seconds, err := strconv.ParseInt(header.Get("Age"), 10, 64); err == nil && seconds >= 0 {
			reportedAge := time.Duration(seconds) * time.Second
			if reportedAge > age {
				age = reportedAge
			}
		}
		return boundClientMetadataTTL(*selectedMaxAge - age)
	}

	if strings.Contains(strings.ToLower(header.Get("Pragma")), "no-cache") || header.Get("Vary") == "*" {
		return 0
	}
	if expires, err := http.ParseTime(header.Get("Expires")); err == nil {
		return boundClientMetadataTTL(expires.Sub(now))
	}
	return defaultClientMetadataTTL
}

func boundClientMetadataTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return 0
	}
	if ttl > maxClientMetadataTTL {
		return maxClientMetadataTTL
	}
	return ttl
}
