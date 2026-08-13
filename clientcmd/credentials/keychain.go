package credentials

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flanksource/incident-commander/auth/oidcclient"
	"github.com/zalando/go-keyring"
)

const (
	keychainService      = "mission-control"
	keychainProbeAccount = "__writable_probe__"
)

// KeychainStore keeps the long-lived secrets — the refresh token and any
// static API token — in the OS keychain, one item per context.
//
// The short-lived access and ID tokens stay in the file store alongside. They
// expire within the hour, so the bounded exposure of a 0600 file is a fair
// trade for keeping the keychain item small: an ID token JWT on its own can
// exceed the ~2560-byte limit Windows credential storage imposes. Keeping it
// small also means the keychain is read once per refresh rather than once per
// command, which on macOS is the difference between one prompt an hour and one
// prompt per invocation.
type KeychainStore struct {
	service   string
	ephemeral *FileStore
}

func NewKeychainStore(dir string) *KeychainStore {
	return &KeychainStore{service: keychainService, ephemeral: NewFileStore(dir)}
}

func (s *KeychainStore) Name() string { return KindKeychain }

type keychainSecret struct {
	Token        string `json:"token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

func (k keychainSecret) isZero() bool { return k.Token == "" && k.RefreshToken == "" }

func (s *KeychainStore) Get(context string) (*Credential, error) {
	cred, err := s.ephemeral.Get(context)
	if err != nil {
		return nil, err
	}

	secret, err := s.readSecret(context)
	if err != nil {
		return nil, err
	}
	if secret.isZero() {
		return cred, nil
	}

	if cred == nil {
		cred = &Credential{}
	}
	cred.Token = secret.Token
	if secret.RefreshToken != "" {
		if cred.OIDC == nil {
			cred.OIDC = &oidcclient.Tokens{}
		}
		cred.OIDC.RefreshToken = secret.RefreshToken
	}
	return cred, nil
}

func (s *KeychainStore) Set(context string, cred *Credential) error {
	if context == "" {
		return fmt.Errorf("context name is required")
	}

	secret := keychainSecret{}
	ephemeral := cred.clone()
	if ephemeral != nil {
		secret.Token = ephemeral.Token
		ephemeral.Token = ""
		if ephemeral.OIDC != nil {
			secret.RefreshToken = ephemeral.OIDC.RefreshToken
			ephemeral.OIDC.RefreshToken = ""
		}
	}

	if err := s.writeSecret(context, secret); err != nil {
		return err
	}
	return s.ephemeral.Set(context, ephemeral)
}

func (s *KeychainStore) Delete(context string) error {
	if err := s.deleteSecret(context); err != nil {
		return err
	}
	return s.ephemeral.Delete(context)
}

// Writable round-trips a probe item through the keychain and the file store.
// On a headless Linux box with no Secret Service this is what reports that the
// keychain is unusable, and it is what Detect keys off at login.
func (s *KeychainStore) Writable() error {
	if err := s.ephemeral.Writable(); err != nil {
		return err
	}
	if err := keyring.Set(s.service, keychainProbeAccount, "probe"); err != nil {
		return fmt.Errorf("keychain is not writable: %w", err)
	}
	if err := keyring.Delete(s.service, keychainProbeAccount); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("keychain is not writable: %w", err)
	}
	return nil
}

func (s *KeychainStore) readSecret(context string) (keychainSecret, error) {
	raw, err := keyring.Get(s.service, context)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return keychainSecret{}, nil
		}
		return keychainSecret{}, fmt.Errorf("read %s from the keychain: %w", context, err)
	}

	var secret keychainSecret
	if err := json.Unmarshal([]byte(raw), &secret); err != nil {
		return keychainSecret{}, fmt.Errorf("parse the keychain item for %s: %w", context, err)
	}
	return secret, nil
}

func (s *KeychainStore) writeSecret(context string, secret keychainSecret) error {
	if secret.isZero() {
		return s.deleteSecret(context)
	}
	raw, err := json.Marshal(secret)
	if err != nil {
		return err
	}
	if err := keyring.Set(s.service, context, string(raw)); err != nil {
		return fmt.Errorf("store %s in the keychain: %w", context, err)
	}
	return nil
}

func (s *KeychainStore) deleteSecret(context string) error {
	if err := keyring.Delete(s.service, context); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("remove %s from the keychain: %w", context, err)
	}
	return nil
}
