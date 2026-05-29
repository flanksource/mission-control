package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/auth/signing"
	"github.com/zitadel/oidc/v3/pkg/op"
)

// Provider wraps the zitadel OpenIDProvider and its http.Handler.
type Provider struct {
	op.OpenIDProvider
	http.Handler
	Storage *Storage
}

// NewProvider creates an OIDC OpenID Provider.
// It loads or generates the RSA signing key, upserts the public key to DB,
// and returns a Provider containing the OpenIDProvider and http.Handler.
func NewProvider(ctx context.Context, issuerURL, signingKeyPath string) (*Provider, error) {
	cryptoKey, err := generateCryptoKey()
	if err != nil {
		return nil, fmt.Errorf("oidc crypto key: %w", err)
	}
	privateKey, keyID, err := loadOrGenerateKey(ctx, signingKeyPath)
	if err != nil {
		return nil, fmt.Errorf("oidc signing key: %w", err)
	}
	return newProviderWithCryptoKey(ctx, issuerURL, cryptoKey, privateKey, keyID)
}

func newProviderWithCryptoKey(ctx context.Context, issuerURL string, cryptoKey [32]byte, privateKey *rsa.PrivateKey, keyID string) (*Provider, error) {
	if err := upsertPublicKey(ctx, keyID, &privateKey.PublicKey); err != nil {
		return nil, fmt.Errorf("upsert public key: %w", err)
	}

	signer := &signingKey{
		id:         keyID,
		algorithm:  "RS256",
		privateKey: privateKey,
	}
	storage := NewStorage(ctx, signer)

	config := &op.Config{
		CryptoKey:             cryptoKey,
		GrantTypeRefreshToken: true,
	}

	oidcProvider, err := op.NewProvider(config, storage,
		op.StaticIssuer(issuerURL),
		op.WithAllowInsecure(), // allow http:// for dev
	)
	if err != nil {
		return nil, fmt.Errorf("create oidc provider: %w", err)
	}

	return &Provider{
		OpenIDProvider: oidcProvider,
		Handler:        oidcProvider,
		Storage:        storage,
	}, nil
}

func loadOrGenerateKey(_ context.Context, keyPath string) (*rsa.PrivateKey, string, error) {
	return signing.Initialize(keyPath)
}

func upsertPublicKey(ctx context.Context, keyID string, pub *rsa.PublicKey) error {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})

	pk := PublicKey{
		ID:        keyID,
		Algorithm: "RS256",
		PublicKey: pemBytes,
		CreatedAt: time.Now(),
	}
	return ctx.DB().Save(&pk).Error
}

func generateCryptoKey() ([32]byte, error) {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return key, fmt.Errorf("generate crypto key: %w", err)
	}
	logger.Warnf("OIDC: generated ephemeral in-memory crypto key")
	return key, nil
}
