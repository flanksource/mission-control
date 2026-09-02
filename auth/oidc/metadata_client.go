package oidc

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

const (
	// maxClientIDLength bounds metadata client identifiers from unauthenticated callers.
	maxClientIDLength = 4096

	maxRedirectURIs      = 10
	maxRedirectURILength = 512
	maxClientNameLength  = 128
)

// clientMetadata is the normalized public-client metadata used by DCR and CIMD.
// Its compact field names keep DCR client identifiers short.
type clientMetadata struct {
	Name         string           `json:"n,omitempty"`
	RedirectURIs []string         `json:"r"`
	IssuedAt     int64            `json:"i,omitempty"`
	GrantTypes   []oidc.GrantType `json:"g,omitempty"`
}

// validateRedirectURIs enforces the redirect URI rules for public clients:
// https anywhere, or plain http only on loopback (RFC 8252 §7.3).
func validateRedirectURIs(uris []string) ([]string, error) {
	if len(uris) == 0 {
		return nil, fmt.Errorf("redirect_uris is required")
	}
	if len(uris) > maxRedirectURIs {
		return nil, fmt.Errorf("at most %d redirect_uris are allowed", maxRedirectURIs)
	}

	validated := make([]string, 0, len(uris))
	for _, raw := range uris {
		if len(raw) > maxRedirectURILength {
			return nil, fmt.Errorf("redirect_uri exceeds %d characters", maxRedirectURILength)
		}

		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("redirect_uri %q is not a valid URL", raw)
		}
		if u.Fragment != "" || strings.Contains(raw, "#") {
			return nil, fmt.Errorf("redirect_uri %q must not contain a fragment", raw)
		}

		switch u.Scheme {
		case "https":
		case "http":
			if !isLoopbackHost(u.Hostname()) {
				return nil, fmt.Errorf("redirect_uri %q must use https unless it is a loopback address", raw)
			}
		default:
			return nil, fmt.Errorf("redirect_uri %q must use the https or http scheme", raw)
		}

		if u.Host == "" {
			return nil, fmt.Errorf("redirect_uri %q must include a host", raw)
		}

		validated = append(validated, raw)
	}

	return validated, nil
}

func isLoopbackHost(host string) bool {
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// IsMetadataClient reports whether clientID identifies a DCR or CIMD client.
func IsMetadataClient(clientID string) bool {
	return IsDynamicClient(clientID) || isClientIDMetadataDocument(clientID)
}

// IsKnownClient reports whether clientID names a client this provider issues tokens for.
func IsKnownClient(clientID string) bool {
	return clientID == ClientID || IsMetadataClient(clientID)
}

// metadataClient is a public client reconstructed from DCR or CIMD metadata.
// It mirrors cliClient except that the metadata supplies its identity and redirects.
type metadataClient struct {
	id       string
	metadata *clientMetadata
}

var _ op.Client = (*metadataClient)(nil)

func (c *metadataClient) GetID() string                    { return c.id }
func (c *metadataClient) RedirectURIs() []string           { return c.metadata.RedirectURIs }
func (c *metadataClient) PostLogoutRedirectURIs() []string { return nil }
func (c *metadataClient) ApplicationType() op.ApplicationType {
	return op.ApplicationTypeNative
}
func (c *metadataClient) AuthMethod() oidc.AuthMethod { return oidc.AuthMethodNone }
func (c *metadataClient) ResponseTypes() []oidc.ResponseType {
	return []oidc.ResponseType{oidc.ResponseTypeCode}
}
func (c *metadataClient) GrantTypes() []oidc.GrantType {
	if len(c.metadata.GrantTypes) > 0 {
		return append([]oidc.GrantType(nil), c.metadata.GrantTypes...)
	}
	return []oidc.GrantType{oidc.GrantTypeCode, oidc.GrantTypeRefreshToken}
}
func (c *metadataClient) LoginURL(id string) string { return "/oidc/login?auth_request_id=" + id }
func (c *metadataClient) AccessTokenType() op.AccessTokenType {
	return op.AccessTokenTypeJWT
}
func (c *metadataClient) IDTokenLifetime() time.Duration { return time.Hour }
func (c *metadataClient) DevMode() bool                  { return false }
func (c *metadataClient) RestrictAdditionalIdTokenScopes() func(scopes []string) []string {
	return func(scopes []string) []string { return scopes }
}
func (c *metadataClient) RestrictAdditionalAccessTokenScopes() func(scopes []string) []string {
	return func(scopes []string) []string { return scopes }
}
func (c *metadataClient) IsScopeAllowed(scope string) bool     { return true }
func (c *metadataClient) IDTokenUserinfoClaimsAssertion() bool { return false }
func (c *metadataClient) ClockSkew() time.Duration             { return 0 }

// DisplayName returns the name to show on the consent screen.
func (c *metadataClient) DisplayName() string {
	if c.metadata.Name != "" {
		return c.metadata.Name
	}
	return "An unnamed application"
}
