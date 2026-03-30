package oidc

import (
	"database/sql/driver"
	"time"

	"github.com/lib/pq"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

const ClientID = "mc-cli"

// StringList is a PostgreSQL text[] compatible type.
type StringList pq.StringArray

func (s StringList) Value() (driver.Value, error) {
	return pq.StringArray(s).Value()
}

func (s *StringList) Scan(src any) error {
	return (*pq.StringArray)(s).Scan(src)
}

// AuthRequest implements op.AuthRequest backed by the oidc_auth_requests table.
type AuthRequest struct {
	ID                  string     `gorm:"primaryKey;column:id"`
	ClientID            string     `gorm:"column:client_id;not null"`
	RedirectURI         string     `gorm:"column:redirect_uri;not null"`
	Scopes              StringList `gorm:"column:scopes;type:text[]"`
	State               string     `gorm:"column:state"`
	Nonce               string     `gorm:"column:nonce"`
	ResponseType        string     `gorm:"column:response_type;not null"`
	CodeChallenge       string     `gorm:"column:code_challenge"`
	CodeChallengeMethod string     `gorm:"column:code_challenge_method"`
	Subject             string     `gorm:"column:subject"`
	AuthTime            *time.Time `gorm:"column:auth_time"`
	Code                *string    `gorm:"column:code"`
	IsDone              bool       `gorm:"column:done;default:false"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	ExpiresAt           time.Time  `gorm:"column:expires_at"`
}

func (AuthRequest) TableName() string { return "oidc_auth_requests" }

func (a *AuthRequest) GetID() string         { return a.ID }
func (a *AuthRequest) GetACR() string        { return "" }
func (a *AuthRequest) GetAMR() []string      { return nil }
func (a *AuthRequest) GetAudience() []string { return []string{a.ClientID} }
func (a *AuthRequest) GetAuthTime() time.Time {
	if a.AuthTime != nil {
		return *a.AuthTime
	}
	return time.Time{}
}
func (a *AuthRequest) GetClientID() string { return a.ClientID }
func (a *AuthRequest) GetCodeChallenge() *oidc.CodeChallenge {
	if a.CodeChallenge == "" {
		return nil
	}
	return &oidc.CodeChallenge{
		Challenge: a.CodeChallenge,
		Method:    oidc.CodeChallengeMethod(a.CodeChallengeMethod),
	}
}
func (a *AuthRequest) GetNonce() string       { return a.Nonce }
func (a *AuthRequest) GetRedirectURI() string { return a.RedirectURI }
func (a *AuthRequest) GetResponseType() oidc.ResponseType {
	return oidc.ResponseType(a.ResponseType)
}
func (a *AuthRequest) GetResponseMode() oidc.ResponseMode { return "" }
func (a *AuthRequest) GetScopes() []string                { return []string(a.Scopes) }
func (a *AuthRequest) GetState() string                   { return a.State }
func (a *AuthRequest) GetSubject() string                 { return a.Subject }
func (a *AuthRequest) Done() bool                         { return a.IsDone }

// RefreshToken is backed by the oidc_refresh_tokens table.
type RefreshToken struct {
	ID         string     `gorm:"primaryKey;column:id"`
	Token      string     `gorm:"column:token;not null;uniqueIndex"`
	ClientID   string     `gorm:"column:client_id;not null"`
	Subject    string     `gorm:"column:subject;not null"`
	Scopes     StringList `gorm:"column:scopes;type:text[]"`
	AuthTime   time.Time  `gorm:"column:auth_time;not null"`
	RotationID string     `gorm:"column:rotation_id;not null"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
	ExpiresAt  time.Time  `gorm:"column:expires_at"`
}

func (RefreshToken) TableName() string { return "oidc_refresh_tokens" }

func (r *RefreshToken) GetAMR() []string       { return nil }
func (r *RefreshToken) GetAudience() []string  { return []string{r.ClientID} }
func (r *RefreshToken) GetAuthTime() time.Time { return r.AuthTime }
func (r *RefreshToken) GetClientID() string    { return r.ClientID }
func (r *RefreshToken) GetScopes() []string    { return []string(r.Scopes) }
func (r *RefreshToken) GetSubject() string     { return r.Subject }
func (r *RefreshToken) SetCurrentScopes(scopes []string) {
	r.Scopes = StringList(scopes)
}

// PublicKey is backed by the oidc_public_keys table.
type PublicKey struct {
	ID        string     `gorm:"primaryKey;column:id"`
	Algorithm string     `gorm:"column:algorithm;default:RS256"`
	PublicKey []byte     `gorm:"column:public_key;type:bytea;not null"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	ExpiresAt *time.Time `gorm:"column:expires_at"`
}

func (PublicKey) TableName() string { return "oidc_public_keys" }

// cliClient is a hardcoded public native client for CLI auth.
type cliClient struct{}

var _ op.Client = (*cliClient)(nil)

func (c *cliClient) GetID() string { return ClientID }
func (c *cliClient) RedirectURIs() []string {
	return []string{
		"https://127.0.0.1/callback", // CLI
		"http://127.0.0.1/callback",  // CLI
		"http://127.0.0.1:33418/",    // vscode
	}
}
func (c *cliClient) PostLogoutRedirectURIs() []string { return nil }
func (c *cliClient) ApplicationType() op.ApplicationType {
	return op.ApplicationTypeNative
}
func (c *cliClient) AuthMethod() oidc.AuthMethod { return oidc.AuthMethodNone }
func (c *cliClient) ResponseTypes() []oidc.ResponseType {
	return []oidc.ResponseType{oidc.ResponseTypeCode}
}
func (c *cliClient) GrantTypes() []oidc.GrantType {
	return []oidc.GrantType{oidc.GrantTypeCode, oidc.GrantTypeRefreshToken}
}
func (c *cliClient) LoginURL(id string) string { return "/oidc/login?auth_request_id=" + id }
func (c *cliClient) AccessTokenType() op.AccessTokenType {
	return op.AccessTokenTypeJWT
}
func (c *cliClient) IDTokenLifetime() time.Duration { return time.Hour }
func (c *cliClient) DevMode() bool                  { return false }
func (c *cliClient) RestrictAdditionalIdTokenScopes() func(scopes []string) []string {
	return func(scopes []string) []string { return scopes }
}
func (c *cliClient) RestrictAdditionalAccessTokenScopes() func(scopes []string) []string {
	return func(scopes []string) []string { return scopes }
}
func (c *cliClient) IsScopeAllowed(scope string) bool     { return true }
func (c *cliClient) IDTokenUserinfoClaimsAssertion() bool { return false }
func (c *cliClient) ClockSkew() time.Duration             { return 0 }
