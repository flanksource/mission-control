package signing

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"github.com/flanksource/commons/logger"
	"github.com/go-jose/go-jose/v4"
)

const (
	DefaultPrivateKeyPath = ".mission-control-signing-key.pem"
	Algorithm             = "RS256"
)

var (
	ErrSigningKeyNotInitialized = errors.New("JWT signing key is not initialized")
	ErrNoPEMBlockFound          = errors.New("no PEM block found")
	ErrTrailingPEMData          = errors.New("trailing data after PEM block")
	ErrUnsupportedPEMBlock      = errors.New("unsupported PEM block type")
	ErrPrivateKeyNotRSA         = errors.New("private key is not RSA")

	PrivateKeyPath string

	privateKey *rsa.PrivateKey
	keyID      string
)

func Initialize(privatePath string) (*rsa.PrivateKey, string, error) {
	if privatePath == "" {
		privatePath = DefaultPrivateKeyPath
	}

	key, err := loadOrGeneratePrivateKey(privatePath)
	if err != nil {
		return nil, "", err
	}

	id, err := KeyID(&key.PublicKey)
	if err != nil {
		return nil, "", err
	}

	PrivateKeyPath = privatePath
	privateKey = key
	keyID = id
	return key, id, nil
}

func PrivateKey() (*rsa.PrivateKey, string, error) {
	if privateKey == nil {
		return nil, "", ErrSigningKeyNotInitialized
	}
	return privateKey, keyID, nil
}

func PublicKey() (*rsa.PublicKey, string, error) {
	key, id, err := PrivateKey()
	if err != nil {
		return nil, "", err
	}
	return &key.PublicKey, id, nil
}

func PublicJWK() (string, error) {
	pub, id, err := PublicKey()
	if err != nil {
		return "", err
	}
	return PublicJWKFromPublicKey(pub, id)
}

func KeyID(pub *rsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:16]), nil
}

func ReadRSAPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseRSAPrivateKey(data)
}

func ParseRSAPrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil {
		return nil, ErrNoPEMBlockFound
	}
	if len(bytes.TrimSpace(rest)) > 0 {
		return nil, ErrTrailingPEMData
	}

	switch block.Type {
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%w: %T", ErrPrivateKeyNotRSA, key)
		}
		return rsaKey, nil
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedPEMBlock, block.Type)
	}
}

func loadOrGeneratePrivateKey(path string) (*rsa.PrivateKey, error) {
	key, err := ReadRSAPrivateKey(path)
	if err == nil {
		logger.Infof("loaded JWT signing key from %s", path)
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read signing key %s: %w", path, err)
	}

	key, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate RSA signing key: %w", err)
	}
	logger.Warnf("JWT signing key %s does not exist; generated an ephemeral in-memory signing key", path)
	return key, nil
}

func PublicJWKFromPublicKey(pub *rsa.PublicKey, id string) (string, error) {
	jwk := jose.JSONWebKey{Key: pub, KeyID: id, Algorithm: Algorithm, Use: "sig"}
	data, err := json.Marshal(jwk)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
