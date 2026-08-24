package mccontext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/auth/oidcclient"
	"github.com/flanksource/incident-commander/clientcmd/credentials"
)

// CredentialStoreEnv overrides the store chosen at login. Read only when a
// context is first created — see MCConfig.CredentialStore.
const CredentialStoreEnv = "MC_CREDENTIAL_STORE"

func (c *MCConfig) store() (credentials.Store, error) {
	return credentials.Open(configDir(), c.CredentialStore)
}

func (c *MCContext) credential() *credentials.Credential {
	if c == nil {
		return nil
	}
	return &credentials.Credential{Token: c.Token, OIDC: cloneOIDCTokens(c.OIDC), NeedsReauth: c.NeedsReauth}
}

func (c *MCContext) applyCredential(cred *credentials.Credential) {
	if c == nil {
		return
	}
	if cred == nil {
		c.Token, c.OIDC, c.NeedsReauth = "", nil, ""
		return
	}
	c.Token = cred.Token
	c.OIDC = cloneOIDCTokens(cred.OIDC)
	c.NeedsReauth = cred.NeedsReauth
}

// hydrateCredentials fills in each context's secrets from the credential store,
// falling back to any still inline in config.json. The store always wins: an
// inline secret is by definition the older copy, and a stale config.json must
// not resurrect a refresh token that has since been rotated.
//
// The snapshot records what the store held, not what was hydrated, so a secret
// that is still inline reads as a change and the next SaveConfig moves it in.
func hydrateCredentials(cfg *MCConfig, inline map[string]*credentials.Credential) error {
	if len(inline) > 0 && cfg.CredentialStore == "" {
		cfg.CredentialStore = credentials.KindFile
	}
	store, err := cfg.store()
	if err != nil {
		return err
	}

	cfg.hydrated = map[string]*credentials.Credential{}
	cfg.loadedStore = cfg.CredentialStore
	for i := range cfg.Contexts {
		name := cfg.Contexts[i].Name
		stored, err := store.Get(name)
		if err != nil {
			return err
		}
		cfg.hydrated[name] = stored
		if stored.IsZero() {
			cfg.Contexts[i].applyCredential(inline[name])
			continue
		}
		cfg.Contexts[i].applyCredential(stored)
	}
	return nil
}

// previousStore is the backend this config was loaded from, and only when it
// differs from the configured one — otherwise there is nothing to move.
func (c *MCConfig) previousStore() (credentials.Store, error) {
	if c.loadedStore == "" || c.loadedStore == c.CredentialStore {
		return nil, nil
	}
	return credentials.Open(configDir(), c.loadedStore)
}

// syncCredentials writes every credential that differs from the hydrated
// snapshot and deletes the ones whose context has been removed. When previous
// is non-nil the backend changed, so every credential moves across. Callers
// must hold the store lock — see SaveConfig.
func (c *MCConfig) syncCredentials(store, previous credentials.Store) error {
	if c.hydrated == nil {
		c.hydrated = map[string]*credentials.Credential{}
	}

	// Leaving credentials behind in the old backend would strand the secrets in
	// a store the config no longer points at, and every context would silently
	// look logged out.
	moved := map[string]*credentials.Credential{}
	if previous != nil {
		logger.Debugf("moving %d credential(s) from the %s store to the %s store", len(c.hydrated), previous.Name(), store.Name())
		moved, c.hydrated = c.hydrated, map[string]*credentials.Credential{}
	}

	live := map[string]bool{}
	for i := range c.Contexts {
		ctx := &c.Contexts[i]
		live[ctx.Name] = true

		cred := ctx.credential()
		same, err := sameCredential(c.hydrated[ctx.Name], cred)
		if err != nil {
			return err
		}
		if same {
			continue
		}
		if err := storeCredential(store, ctx.Name, cred); err != nil {
			return err
		}
		c.hydrated[ctx.Name] = cred
	}

	for name := range c.hydrated {
		if live[name] {
			continue
		}
		if err := store.Delete(name); err != nil {
			return fmt.Errorf("remove credential for context %q from the %s store: %w", name, store.Name(), err)
		}
		delete(c.hydrated, name)
	}

	// Only once every credential is safely in the new store.
	for name := range moved {
		if err := previous.Delete(name); err != nil {
			return fmt.Errorf("remove credential for context %q from the %s store: %w", name, previous.Name(), err)
		}
	}
	c.loadedStore = c.CredentialStore
	return nil
}

func storeCredential(store credentials.Store, name string, cred *credentials.Credential) error {
	if cred.IsZero() {
		if err := store.Delete(name); err != nil {
			return fmt.Errorf("remove credential for context %q from the %s store: %w", name, store.Name(), err)
		}
		return nil
	}
	if err := store.Set(name, cred); err != nil {
		return fmt.Errorf("save credential for context %q in the %s store: %w", name, store.Name(), err)
	}
	return nil
}

// sameCredential compares the serialised form so that a token time that carries
// a monotonic clock reading does not read as a change.
func sameCredential(a, b *credentials.Credential) (bool, error) {
	if a.IsZero() && b.IsZero() {
		return true, nil
	}
	left, err := json.Marshal(a)
	if err != nil {
		return false, err
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(left, right), nil
}

// ChooseCredentialStore records where this config keeps secrets. It runs at
// login and nowhere else — see MCConfig.CredentialStore.
//
// The file store is the default: it works everywhere, and it is what every
// existing install already does. The keychain is opt-in, because moving a
// user's secrets there behind their back also signs them up for a keychain
// prompt on every rebuilt binary. A keychain that was asked for and does not
// work is an error, never a silent downgrade to plaintext.
func ChooseCredentialStore(cfg *MCConfig, requested string) error {
	if requested == "" {
		requested = os.Getenv(CredentialStoreEnv)
	}
	if requested == "" {
		if cfg.CredentialStore != "" {
			return nil
		}
		requested = credentials.KindFile
	}

	store, err := credentials.Open(configDir(), requested)
	if err != nil {
		return err
	}
	if err := store.Writable(); err != nil {
		return fmt.Errorf("credential store %q is not usable: %w", store.Name(), err)
	}
	cfg.CredentialStore = store.Name()
	return nil
}

// legacyConfig sees the pre-store shape of config.json — secrets inline in each
// context — which MCContext deliberately no longer unmarshals.
type legacyConfig struct {
	Contexts []struct {
		Name  string             `json:"name"`
		Token string             `json:"token"`
		OIDC  *oidcclient.Tokens `json:"oidc"`
	} `json:"contexts"`
}

// inlineCredentials returns the secrets still written inline in config.json,
// keyed by context name.
func inlineCredentials(data []byte) (map[string]*credentials.Credential, error) {
	var legacy legacyConfig
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	inline := map[string]*credentials.Credential{}
	for _, ctx := range legacy.Contexts {
		cred := &credentials.Credential{Token: ctx.Token, OIDC: ctx.OIDC}
		if ctx.Name != "" && !cred.IsZero() {
			inline[ctx.Name] = cred
		}
	}
	return inline, nil
}

// warnInlineOnce keeps the warning to one line per process: LoadConfig runs a
// dozen times over a single command.
var warnInlineOnce sync.Once

// migrateInlineCredentials moves the secrets hydrated out of config.json into
// the credential store and rewrites config.json without them. It migrates to
// whatever store the config records — the file store by default — rather than
// probing for a keychain: the secrets are already plaintext on disk, so this
// preserves the user's posture instead of silently changing it (and prompting
// them). Opting into the keychain is `auth login --credential-store keychain`.
//
// A failure here is not fatal. The secrets are exactly where they already were,
// so the config stays usable read-only — a read-only config mount and a
// CI container with a baked-in token both keep working. Nothing is silently
// degraded: refreshContextToken still refuses to spend a rotating credential it
// cannot store.
func migrateInlineCredentials(cfg *MCConfig) {
	logger.Debugf("migrating %d inline credential(s) out of %s into the %s store", len(cfg.hydrated), configPath(), cfg.CredentialStore)
	if err := SaveConfig(cfg); err != nil {
		warnInlineOnce.Do(func() {
			logger.Warnf("could not move the credentials in %s into the %s store, so they stay in that file: %v", configPath(), cfg.CredentialStore, err)
		})
	}
}
