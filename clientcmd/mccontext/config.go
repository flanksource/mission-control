// Package mccontext resolves the Mission Control client context — the server a
// command talks to and the credential it talks with — and builds an SDK client
// bound to it.
//
// It is deliberately a leaf: the only in-repo packages it may import are sdk,
// auth/oidcclient, auth/oidc/static and clientcmd/credentials. The cobra
// commands that drive it live in clientcmd, which cannot be imported outside
// this module because it reaches the plugin machinery, and plugin/api resolves
// only through a filesystem replace. Anything that needs a Mission Control
// client and nothing else — including other Flanksource binaries — imports this
// instead.
package mccontext

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/incident-commander/auth/oidcclient"
	"github.com/flanksource/incident-commander/clientcmd/credentials"
)

type MCContext struct {
	Name       string                `json:"name"`
	Server     string                `json:"server,omitempty"`
	DB         string                `json:"db,omitempty"`
	Endpoints  *oidcclient.Discovery `json:"endpoints,omitempty"`
	Properties map[string]string     `json:"properties,omitempty"`

	// Token, OIDC and NeedsReauth are secrets and live in the credential store,
	// never in config.json. LoadConfig hydrates them; SaveConfig writes them
	// back.
	Token       string             `json:"-"`
	OIDC        *oidcclient.Tokens `json:"-"`
	NeedsReauth string             `json:"-"`
}

type MCConfig struct {
	CurrentContext string `json:"current_context"`

	// CredentialStore is chosen once, at login, and honoured verbatim
	// afterwards. Re-detecting it at runtime would silently write a plaintext
	// refresh token for a user who asked for the keychain.
	CredentialStore string `json:"credential_store,omitempty"`

	Contexts []MCContext `json:"contexts"`

	// hydrated is what the credential store held when this config was loaded,
	// and loadedStore is which store that was. SaveConfig diffs against them to
	// write only the credentials that changed, to delete the ones whose context
	// is gone, and to move every credential across when the backend changes.
	hydrated    map[string]*credentials.Credential
	loadedStore string
}

// ContextFlag selects a context other than the configured current one. The CLI
// binds `--context` straight to it; a library caller assigns it before loading.
// Exported rather than hidden behind a setter because cobra needs the address.
var ContextFlag string

func configDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "mission-control")
}

func configPath() string {
	return filepath.Join(configDir(), "config.json")
}

// ConfigDir is where config.json and the profile cache live, and ConfigPath is
// config.json itself. Exported for the commands that report or manage them.
func ConfigDir() string  { return configDir() }
func ConfigPath() string { return configPath() }

func ProfileDir(namespace, name string) string {
	return filepath.Join(configDir(), "profiles", namespace+"_"+name)
}

// LoadConfig reads config.json and hydrates each context's secrets from the
// credential store, migrating any secrets still inline in config.json first.
func LoadConfig() (*MCConfig, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &MCConfig{}, nil
		}
		return nil, err
	}
	var cfg MCConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	inline, err := inlineCredentials(data)
	if err != nil {
		return nil, err
	}
	if err := hydrateCredentials(&cfg, inline); err != nil {
		return nil, err
	}
	if len(inline) > 0 {
		migrateInlineCredentials(&cfg)
	}
	return &cfg, nil
}

// SaveConfig persists config.json and any credential that differs from what the
// store held at load. It is the only save path: a caller that mutates a
// context's secret and saves must not be able to lose it silently.
func SaveConfig(cfg *MCConfig) error {
	store, err := cfg.store()
	if err != nil {
		return err
	}
	previous, err := cfg.previousStore()
	if err != nil {
		return err
	}
	return credentials.WithLock(configDir(), func() error {
		if err := cfg.syncCredentials(store, previous); err != nil {
			return err
		}
		return saveConfigLocked(cfg)
	})
}

func saveConfigLocked(cfg *MCConfig) error {
	if err := os.MkdirAll(configDir(), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return credentials.WriteAtomic(configPath(), data)
}

func (c *MCConfig) GetContext(name string) *MCContext {
	for i := range c.Contexts {
		if c.Contexts[i].Name == name {
			return &c.Contexts[i]
		}
	}
	return nil
}

func (c *MCConfig) SetContext(ctx MCContext) {
	for i := range c.Contexts {
		if c.Contexts[i].Name == ctx.Name {
			c.Contexts[i] = ctx
			return
		}
	}
	c.Contexts = append(c.Contexts, ctx)
}

func (c *MCConfig) RemoveContext(name string) bool {
	for i := range c.Contexts {
		if c.Contexts[i].Name == name {
			c.Contexts = append(c.Contexts[:i], c.Contexts[i+1:]...)
			if c.CurrentContext == name {
				c.CurrentContext = ""
				if len(c.Contexts) == 1 {
					c.CurrentContext = c.Contexts[0].Name
				}
			}
			return true
		}
	}
	return false
}

func (c *MCConfig) CurrentMCContext() *MCContext {
	if ContextFlag != "" {
		return c.GetContext(ContextFlag)
	}
	if c.CurrentContext == "" {
		return nil
	}
	return c.GetContext(c.CurrentContext)
}

func ServerToContextName(serverURL string) string {
	u, err := url.Parse(serverURL)
	if err != nil {
		return strings.NewReplacer("://", "_", "/", "_", ":", "_").Replace(serverURL)
	}
	return u.Hostname()
}

func ContextHasAPI() (*MCContext, bool) {
	cfg, _ := LoadConfig()
	if cfg == nil {
		return nil, false
	}
	ctx := cfg.CurrentMCContext()
	return ctx, ctx != nil && ctx.Server != "" && ctx.HasAuth()
}

// HasAuth reports whether the context holds a credential that can still work.
// A refresh token the server has already rejected does not count — see
// NeedsReauth.
func (c *MCContext) HasAuth() bool {
	if c == nil || c.NeedsReauth != "" {
		return false
	}
	return c.AccessToken() != "" || (c.OIDC != nil && c.OIDC.RefreshToken != "")
}

// ReauthError is the one message a user with a dead credential should see: what
// is wrong, and the exact command that fixes it.
func (c *MCContext) ReauthError() error {
	return fmt.Errorf("context %q needs re-authentication (%s)\n  run: %s auth login --server %s",
		c.Name, c.NeedsReauth, filepath.Base(os.Args[0]), c.Server)
}

func (c *MCContext) AccessToken() string {
	if c == nil {
		return ""
	}
	if c.OIDC != nil && c.OIDC.AccessToken != "" {
		return c.OIDC.AccessToken
	}
	return c.Token
}

func (c *MCContext) SetOIDCTokens(tokens *oidcclient.Tokens) {
	if c == nil {
		return
	}
	c.OIDC = cloneOIDCTokens(tokens)
	c.NeedsReauth = ""
	if tokens != nil && tokens.AccessToken != "" {
		c.Token = ""
	}
}

func cloneOIDCTokens(tokens *oidcclient.Tokens) *oidcclient.Tokens {
	if tokens == nil {
		return nil
	}
	clone := *tokens
	return &clone
}
