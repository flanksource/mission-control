package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
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
	privateKey, keyID, err := loadOrGenerateKey(ctx, signingKeyPath)
	if err != nil {
		return nil, fmt.Errorf("oidc signing key: %w", err)
	}

	signer := &signingKey{
		id:         keyID,
		algorithm:  "RS256",
		privateKey: privateKey,
	}
	storage := NewStorage(ctx, signer)

	config := &op.Config{
		CryptoKey: aesKeyFromIssuer(issuerURL),
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

func loadOrGenerateKey(ctx context.Context, keyPath string) (*rsa.PrivateKey, string, error) {
	if keyPath == "" {
		keyPath = "oidc-signing-key.pem"
	}

	data, err := os.ReadFile(keyPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("read key file %s: %w", keyPath, err)
	}

	var privateKey *rsa.PrivateKey
	if len(data) > 0 {
		privateKey, err = parseRSAPrivateKey(data)
		if err != nil {
			return nil, "", fmt.Errorf("parse signing key: %w", err)
		}
		logger.Infof("OIDC: loaded signing key from %s", keyPath)
	} else {
		privateKey, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, "", fmt.Errorf("generate RSA key: %w", err)
		}
		if err := writeRSAPrivateKey(keyPath, privateKey); err != nil {
			return nil, "", fmt.Errorf("write signing key: %w", err)
		}
		logger.Infof("OIDC: generated new signing key at %s", keyPath)
	}

	keyID, err := generateKeyID(&privateKey.PublicKey)
	if err != nil {
		return nil, "", err
	}

	if err := upsertPublicKey(ctx, keyID, &privateKey.PublicKey); err != nil {
		return nil, "", fmt.Errorf("upsert public key: %w", err)
	}

	return privateKey, keyID, nil
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

func parseRSAPrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func writeRSAPrivateKey(path string, key *rsa.PrivateKey) error {
	data := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return os.WriteFile(path, data, 0600)
}

// aesKeyFromIssuer derives a 32-byte AES key from the issuer URL for OIDC internal encryption.
func aesKeyFromIssuer(issuer string) [32]byte {
	var key [32]byte
	copy(key[:], []byte(issuer))
	return key
}
