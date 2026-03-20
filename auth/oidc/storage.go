package oidc

import (
	gocontext "context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
	"gorm.io/gorm"
)

type signingKey struct {
	id         string
	algorithm  jose.SignatureAlgorithm
	privateKey *rsa.PrivateKey
}

func (s *signingKey) SignatureAlgorithm() jose.SignatureAlgorithm { return s.algorithm }
func (s *signingKey) Key() any                                    { return s.privateKey }
func (s *signingKey) ID() string                                  { return s.id }

type publicKey struct {
	id        string
	algorithm jose.SignatureAlgorithm
	key       *rsa.PublicKey
}

func (p *publicKey) ID() string                         { return p.id }
func (p *publicKey) Algorithm() jose.SignatureAlgorithm { return p.algorithm }
func (p *publicKey) Use() string                        { return "sig" }
func (p *publicKey) Key() any                           { return p.key }

// Storage implements op.Storage backed by Postgres.
type Storage struct {
	ctx    context.Context
	signer *signingKey
}

var _ op.Storage = (*Storage)(nil)

func NewStorage(ctx context.Context, signer *signingKey) *Storage {
	return &Storage{ctx: ctx, signer: signer}
}

func (s *Storage) Health(_ gocontext.Context) error { return nil }

func (s *Storage) CreateAuthRequest(_ gocontext.Context, req *oidc.AuthRequest, _ string) (op.AuthRequest, error) {
	ar := &AuthRequest{
		ID:                  uuid.New().String(),
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		Scopes:              StringList(req.Scopes),
		State:               req.State,
		Nonce:               req.Nonce,
		ResponseType:        string(req.ResponseType),
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: string(req.CodeChallengeMethod),
		CreatedAt:           time.Now(),
		ExpiresAt:           time.Now().Add(10 * time.Minute),
	}
	if err := s.ctx.DB().Create(ar).Error; err != nil {
		return nil, fmt.Errorf("create auth request: %w", err)
	}
	return ar, nil
}

func (s *Storage) AuthRequestByID(_ gocontext.Context, id string) (op.AuthRequest, error) {
	var ar AuthRequest
	if err := s.ctx.DB().Where("id = ? AND expires_at > NOW()", id).First(&ar).Error; err != nil {
		return nil, fmt.Errorf("auth request not found: %w", err)
	}
	return &ar, nil
}

func (s *Storage) AuthRequestByCode(_ gocontext.Context, code string) (op.AuthRequest, error) {
	var ar AuthRequest
	if err := s.ctx.DB().Where("code = ? AND expires_at > NOW()", code).First(&ar).Error; err != nil {
		return nil, fmt.Errorf("auth request not found: %w", err)
	}
	return &ar, nil
}

func (s *Storage) SaveAuthCode(_ gocontext.Context, id, code string) error {
	return s.ctx.DB().Model(&AuthRequest{}).Where("id = ?", id).
		Updates(map[string]any{"code": code, "done": true}).Error
}

func (s *Storage) DeleteAuthRequest(_ gocontext.Context, id string) error {
	return s.ctx.DB().Where("id = ?", id).Delete(&AuthRequest{}).Error
}

func (s *Storage) CreateAccessToken(_ gocontext.Context, req op.TokenRequest) (string, time.Time, error) {
	expiry := time.Now().Add(time.Hour)
	return uuid.New().String(), expiry, nil
}

func (s *Storage) CreateAccessAndRefreshTokens(_ gocontext.Context, req op.TokenRequest, currentRefreshToken string) (string, string, time.Time, error) {
	accessTokenID := uuid.New().String()
	expiry := time.Now().Add(time.Hour)

	rotationID := uuid.New().String()
	if currentRefreshToken != "" {
		// find existing rotation family
		var existing RefreshToken
		if err := s.ctx.DB().Where("token = ?", currentRefreshToken).First(&existing).Error; err == nil {
			rotationID = existing.RotationID
			// rotate: mark old token expired
			s.ctx.DB().Model(&RefreshToken{}).Where("token = ?", currentRefreshToken).
				Update("expires_at", time.Now())
		}
	}

	ar, ok := req.(*AuthRequest)
	if !ok {
		return "", "", time.Time{}, fmt.Errorf("unexpected request type %T", req)
	}

	now := time.Now()
	rt := &RefreshToken{
		ID:         uuid.New().String(),
		Token:      uuid.New().String(),
		ClientID:   ar.ClientID,
		Subject:    ar.Subject,
		Scopes:     ar.Scopes,
		AuthTime:   now,
		RotationID: rotationID,
		CreatedAt:  now,
		ExpiresAt:  now.Add(30 * 24 * time.Hour),
	}
	if err := s.ctx.DB().Create(rt).Error; err != nil {
		return "", "", time.Time{}, fmt.Errorf("create refresh token: %w", err)
	}

	return accessTokenID, rt.Token, expiry, nil
}

func (s *Storage) TokenRequestByRefreshToken(_ gocontext.Context, refreshToken string) (op.RefreshTokenRequest, error) {
	var rt RefreshToken
	if err := s.ctx.DB().Where("token = ? AND expires_at > NOW()", refreshToken).First(&rt).Error; err != nil {
		return nil, op.ErrInvalidRefreshToken
	}
	return &rt, nil
}

func (s *Storage) TerminateSession(_ gocontext.Context, userID, _ string) error {
	return s.ctx.DB().Where("subject = ?", userID).Delete(&RefreshToken{}).Error
}

func (s *Storage) RevokeToken(_ gocontext.Context, tokenOrID, userID, _ string) *oidc.Error {
	var query *gorm.DB
	if userID != "" {
		query = s.ctx.DB().Where("id = ? AND subject = ?", tokenOrID, userID)
	} else {
		query = s.ctx.DB().Where("token = ?", tokenOrID)
	}
	if err := query.Delete(&RefreshToken{}).Error; err != nil {
		return oidc.ErrServerError()
	}
	return nil
}

func (s *Storage) GetRefreshTokenInfo(_ gocontext.Context, _, token string) (string, string, error) {
	var rt RefreshToken
	if err := s.ctx.DB().Where("token = ?", token).First(&rt).Error; err != nil {
		return "", "", op.ErrInvalidRefreshToken
	}
	return rt.Subject, rt.ID, nil
}

func (s *Storage) SigningKey(_ gocontext.Context) (op.SigningKey, error) {
	return s.signer, nil
}

func (s *Storage) SignatureAlgorithms(_ gocontext.Context) ([]jose.SignatureAlgorithm, error) {
	return []jose.SignatureAlgorithm{jose.RS256}, nil
}

func (s *Storage) KeySet(_ gocontext.Context) ([]op.Key, error) {
	var keys []PublicKey
	if err := s.ctx.DB().Where("expires_at IS NULL OR expires_at > NOW()").Find(&keys).Error; err != nil {
		return nil, err
	}

	result := make([]op.Key, 0, len(keys))
	for _, k := range keys {
		pub, err := parseRSAPublicKey(k.PublicKey)
		if err != nil {
			continue
		}
		result = append(result, &publicKey{
			id:        k.ID,
			algorithm: jose.RS256,
			key:       pub,
		})
	}
	return result, nil
}

func (s *Storage) GetClientByClientID(_ gocontext.Context, clientID string) (op.Client, error) {
	if clientID != ClientID {
		return nil, fmt.Errorf("unknown client: %s", clientID)
	}
	return &cliClient{}, nil
}

func (s *Storage) AuthorizeClientIDSecret(_ gocontext.Context, _, _ string) error {
	return nil // public client, no secret
}

func (s *Storage) SetUserinfoFromScopes(_ gocontext.Context, _ *oidc.UserInfo, _, _ string, _ []string) error {
	return nil // deprecated
}

func (s *Storage) SetUserinfoFromToken(_ gocontext.Context, userinfo *oidc.UserInfo, _, subject, _ string) error {
	return s.populateUserinfo(userinfo, subject)
}

func (s *Storage) SetIntrospectionFromToken(_ gocontext.Context, resp *oidc.IntrospectionResponse, _, subject, _ string) error {
	resp.Subject = subject
	return nil
}

func (s *Storage) GetPrivateClaimsFromScopes(_ gocontext.Context, _, _ string, _ []string) (map[string]any, error) {
	return nil, nil
}

func (s *Storage) GetKeyByIDAndClientID(_ gocontext.Context, _, _ string) (*jose.JSONWebKey, error) {
	return nil, nil
}

func (s *Storage) ValidateJWTProfileScopes(_ gocontext.Context, _ string, scopes []string) ([]string, error) {
	return scopes, nil
}

func (s *Storage) populateUserinfo(userinfo *oidc.UserInfo, subject string) error {
	var person models.Person
	if err := s.ctx.DB().Where("id = ?", subject).First(&person).Error; err != nil {
		return err
	}
	userinfo.Subject = subject
	userinfo.Name = person.Name
	userinfo.Email = person.Email
	userinfo.EmailVerified = oidc.Bool(true)
	return nil
}

// SetAuthRequestSubject sets the subject on an auth request after login.
func (s *Storage) SetAuthRequestSubject(id, subject string) error {
	now := time.Now()
	return s.ctx.DB().Model(&AuthRequest{}).Where("id = ?", id).
		Updates(map[string]any{
			"subject":   subject,
			"auth_time": now,
			"done":      true,
		}).Error
}

// CleanupExpired removes expired auth requests and refresh tokens.
func (s *Storage) CleanupExpired() error {
	if err := s.ctx.DB().Where("expires_at < NOW()").Delete(&AuthRequest{}).Error; err != nil {
		return err
	}
	return s.ctx.DB().Where("expires_at < NOW()").Delete(&RefreshToken{}).Error
}

func parseRSAPublicKey(pemData []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}
	return rsaPub, nil
}

// generateKeyID returns a deterministic key ID from the public key.
func generateKeyID(pub *rsa.PublicKey) (string, error) {
	b, err := json.Marshal(pub)
	if err != nil {
		// fallback
		return uuid.New().String(), nil
	}
	h := crypto.SHA256.New()
	h.Write(b)
	return fmt.Sprintf("%x", h.Sum(nil))[:16], nil
}
