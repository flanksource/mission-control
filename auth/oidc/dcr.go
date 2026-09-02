package oidc

// RFC 7591 Dynamic Client Registration.
//
// MCP clients (Claude Desktop, Codex CLI, VS Code) cannot be told a client_id —
// they discover the authorization server and register themselves. Registration
// is therefore open and unauthenticated, which rules out storing every
// registration: a bad actor could grow the table without bound.
//
// Instead the client_id *is* the client metadata, base64url encoded. Nothing is
// persisted at registration time and lookups are a pure decode. The encoding is
// deliberately unsigned: /register is open, so a hand-crafted client_id can only
// express something the same caller could have obtained by asking. To keep that
// true, decodeClientID re-runs the exact validation performed at registration —
// anything that would have been rejected by POST /register is equally rejected
// on lookup.
//
// A client_id grants nothing on its own. Access requires a user to complete an
// interactive login, approve the client on the consent screen, and hold the
// mcp:use permission. Live grants are the refresh tokens in oidc_refresh_tokens,
// which are already revocable and garbage collected.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

const (
	// RegistrationEndpoint is the RFC 7591 client registration endpoint. It sits
	// under /oauth/ so it inherits the same auth-skip prefix as /oauth/token.
	RegistrationEndpoint = "/oauth/register"

	dynamicClientIDPrefix = "dcr_"

	// maxClientIDLength bounds metadata client identifiers from unauthenticated callers.
	maxClientIDLength = 4096

	maxRedirectURIs      = 10
	maxRedirectURILength = 512
	maxClientNameLength  = 128
	maxRegistrationBody  = 16 << 10
)

// clientMetadata is the normalized public-client metadata used by DCR and CIMD.
// Its compact field names keep DCR client identifiers short.
type clientMetadata struct {
	Name         string           `json:"n,omitempty"`
	RedirectURIs []string         `json:"r"`
	IssuedAt     int64            `json:"i,omitempty"`
	GrantTypes   []oidc.GrantType `json:"g,omitempty"`
}

// registrationRequest is the RFC 7591 client metadata document sent by clients.
type registrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

// registrationResponse is the RFC 7591 client information response.
type registrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope,omitempty"`
}

// registrationError is the RFC 7591 §3.2.2 error response.
type registrationError struct {
	Error       string `json:"error"`
	Description string `json:"error_description,omitempty"`
}

func mountRegistrationRoutes(e *echo.Echo) {
	e.POST(RegistrationEndpoint, registrationHandler)
	e.OPTIONS(RegistrationEndpoint, func(c echo.Context) error {
		setOAuthCORSHeaders(c.Response().Header())
		return c.NoContent(http.StatusNoContent)
	})
}

func registrationHandler(c echo.Context) error {
	setOAuthCORSHeaders(c.Response().Header())

	var req registrationRequest
	if err := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, maxRegistrationBody)).Decode(&req); err != nil {
		return registrationFailure(c, "invalid_client_metadata", "request body is not valid JSON")
	}

	redirectURIs, err := validateRedirectURIs(req.RedirectURIs)
	if err != nil {
		return registrationFailure(c, "invalid_redirect_uri", err.Error())
	}

	// Only public clients are supported: MCP clients run on the user's machine
	// and cannot hold a secret, so no client_secret is ever issued.
	if m := req.TokenEndpointAuthMethod; m != "" && m != string(oidc.AuthMethodNone) {
		return registrationFailure(c, "invalid_client_metadata",
			fmt.Sprintf("only token_endpoint_auth_method %q is supported", oidc.AuthMethodNone))
	}

	for _, gt := range req.GrantTypes {
		if gt != string(oidc.GrantTypeCode) && gt != string(oidc.GrantTypeRefreshToken) {
			return registrationFailure(c, "invalid_client_metadata", fmt.Sprintf("unsupported grant_type %q", gt))
		}
	}

	for _, rt := range req.ResponseTypes {
		if rt != string(oidc.ResponseTypeCode) {
			return registrationFailure(c, "invalid_client_metadata", fmt.Sprintf("unsupported response_type %q", rt))
		}
	}

	name := strings.TrimSpace(req.ClientName)
	if len(name) > maxClientNameLength {
		name = name[:maxClientNameLength]
	}

	clientID, err := encodeClientID(clientMetadata{
		Name:         name,
		RedirectURIs: redirectURIs,
		IssuedAt:     time.Now().Unix(),
	})
	if err != nil {
		return registrationFailure(c, "invalid_client_metadata", "client metadata is too large")
	}

	return c.JSON(http.StatusCreated, registrationResponse{
		ClientID:                clientID,
		ClientIDIssuedAt:        time.Now().Unix(),
		ClientName:              name,
		RedirectURIs:            redirectURIs,
		GrantTypes:              []string{string(oidc.GrantTypeCode), string(oidc.GrantTypeRefreshToken)},
		ResponseTypes:           []string{string(oidc.ResponseTypeCode)},
		TokenEndpointAuthMethod: string(oidc.AuthMethodNone),
		Scope:                   "openid profile email offline_access",
	})
}

func registrationFailure(c echo.Context, code, description string) error {
	return c.JSON(http.StatusBadRequest, registrationError{Error: code, Description: description})
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

func encodeClientID(md clientMetadata) (string, error) {
	payload, err := json.Marshal(md)
	if err != nil {
		return "", err
	}

	id := dynamicClientIDPrefix + base64.RawURLEncoding.EncodeToString(payload)
	if len(id) > maxClientIDLength {
		return "", fmt.Errorf("encoded client_id exceeds %d characters", maxClientIDLength)
	}
	return id, nil
}

// decodeClientID reverses encodeClientID. It re-applies registration-time
// validation so that a hand-crafted client_id can never express more than
// POST /register would have handed out.
func decodeClientID(clientID string) (*clientMetadata, error) {
	if !strings.HasPrefix(clientID, dynamicClientIDPrefix) {
		return nil, fmt.Errorf("not a dynamically registered client")
	}
	if len(clientID) > maxClientIDLength {
		return nil, fmt.Errorf("client_id exceeds %d characters", maxClientIDLength)
	}

	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(clientID, dynamicClientIDPrefix))
	if err != nil {
		return nil, fmt.Errorf("client_id is not valid base64url")
	}

	var md clientMetadata
	if err := json.Unmarshal(payload, &md); err != nil {
		return nil, fmt.Errorf("client_id does not carry valid client metadata")
	}

	redirectURIs, err := validateRedirectURIs(md.RedirectURIs)
	if err != nil {
		return nil, err
	}
	md.RedirectURIs = redirectURIs

	if len(md.Name) > maxClientNameLength {
		md.Name = md.Name[:maxClientNameLength]
	}

	return &md, nil
}

// IsDynamicClient reports whether clientID contains valid dynamic registration metadata.
func IsDynamicClient(clientID string) bool {
	_, err := decodeClientID(clientID)
	return err == nil
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
