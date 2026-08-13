package clientcmd

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/flanksource/incident-commander/auth/oidcclient"
	"github.com/flanksource/incident-commander/clientcmd/credentials"
	"github.com/spf13/cobra"
)

type MCContext struct {
	Name      string `json:"name"`
	Server    string `json:"server,omitempty"`
	DB        string `json:"db,omitempty"`
	Endpoints *oidcclient.Discovery `json:"endpoints,omitempty"`
	Properties map[string]string    `json:"properties,omitempty"`

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

var contextFlag string

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
	if contextFlag != "" {
		return c.GetContext(contextFlag)
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

var ContextCmd = &cobra.Command{
	Use:   "context",
	Short: "Manage Mission Control contexts",
}

var contextUseCmd = &cobra.Command{
	Use:   "use [name]",
	Short: "Switch the current context",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := LoadConfig()
		if err != nil {
			return err
		}

		var name string
		if len(args) > 0 {
			name = args[0]
		} else {
			if len(cfg.Contexts) == 0 {
				return fmt.Errorf("no contexts configured")
			}
			options := make([]huh.Option[string], len(cfg.Contexts))
			for i, c := range cfg.Contexts {
				label := c.Name
				if c.Name == cfg.CurrentContext {
					label += " (current)"
				}
				if c.Server != "" {
					label += "  " + c.Server
				}
				options[i] = huh.NewOption(label, c.Name)
			}
			if err := huh.NewSelect[string]().
				Title("Select context").
				Options(options...).
				Value(&name).
				Run(); err != nil {
				return err
			}
		}

		if cfg.GetContext(name) == nil {
			return fmt.Errorf("context %q not found", name)
		}

		cfg.CurrentContext = name
		if err := SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Switched to context %q\n", name)
		return nil
	},
}

var contextListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all contexts",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := LoadConfig()
		if err != nil {
			return err
		}
		if len(cfg.Contexts) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No contexts configured")
			return nil
		}
		for _, c := range cfg.Contexts {
			marker := "  "
			if c.Name == cfg.CurrentContext {
				marker = "* "
			}
			info := c.Server
			if info == "" && c.DB != "" {
				info = "(db only)"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s%s\t%s\n", marker, c.Name, info)
		}
		return nil
	},
}

var contextRemoveCmd = &cobra.Command{
	Use:     "remove [name]",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove a Mission Control context",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := LoadConfig()
		if err != nil {
			return err
		}

		var name string
		if len(args) > 0 {
			name = args[0]
		} else {
			if len(cfg.Contexts) == 0 {
				return fmt.Errorf("no contexts configured")
			}
			options := make([]huh.Option[string], len(cfg.Contexts))
			for i, c := range cfg.Contexts {
				label := c.Name
				if c.Name == cfg.CurrentContext {
					label += " (current)"
				}
				if c.Server != "" {
					label += "  " + c.Server
				}
				options[i] = huh.NewOption(label, c.Name)
			}
			if err := huh.NewSelect[string]().
				Title("Remove context").
				Options(options...).
				Value(&name).
				Run(); err != nil {
				return err
			}
		}

		previousCurrent := cfg.CurrentContext
		if !cfg.RemoveContext(name) {
			return fmt.Errorf("context %q not found", name)
		}
		if err := SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Removed context %q\n", name)
		if previousCurrent == name && cfg.CurrentContext != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Switched to context %q\n", cfg.CurrentContext)
		}
		return nil
	},
}

var contextCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the current context",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := LoadConfig()
		if err != nil {
			return err
		}
		ctx := cfg.CurrentMCContext()
		if ctx == nil {
			fmt.Fprintln(cmd.OutOrStdout(), "No current context")
			return nil
		}
		data, _ := json.MarshalIndent(ctx, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	},
}

var (
	contextAddName   string
	contextAddServer string
	contextAddDB     string
	contextAddToken  string
	contextAddUse    bool
)

var contextAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add or update a Mission Control context",
	Long: `Add a new context (or update an existing one by name). At least one of --server
or --db-url is required. Pass --use to switch to the new context immediately.

Examples:
  mission-control context add --name local --db-url "$DB_URL"
  mission-control context add --name beta --server https://beta.flanksource.com --token "$TOKEN" --use`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if contextAddName == "" {
			return fmt.Errorf("--name is required")
		}
		if contextAddServer == "" && contextAddDB == "" {
			return fmt.Errorf("at least one of --server or --db-url is required")
		}

		cfg, err := LoadConfig()
		if err != nil {
			return err
		}

		existingCtx := cfg.GetContext(contextAddName)
		existing := existingCtx != nil
		ctx := MCContext{Name: contextAddName}
		if existingCtx != nil {
			ctx = *existingCtx
		}
		if cmd.Flags().Changed("server") {
			server, err := ResolveAPIBase(contextAddServer)
			if err != nil {
				return err
			}
			ctx.Server = server
			if !cmd.Flags().Changed("token") {
				ctx.Token = ""
				ctx.OIDC = nil
				ctx.NeedsReauth = ""
			}
		}
		if cmd.Flags().Changed("db-url") {
			ctx.DB = contextAddDB
		}
		if cmd.Flags().Changed("token") {
			ctx.Token = contextAddToken
			ctx.OIDC = nil
			ctx.NeedsReauth = ""
		}
		if err := ChooseCredentialStore(cfg, ""); err != nil {
			return err
		}
		if ctx.Server != "" && !cmd.Flags().Changed("token") && !ctx.HasAuth() {
			if err := EnsureContextToken(cmd, &ctx, cmd.ErrOrStderr()); err != nil {
				return err
			}
		}
		cfg.SetContext(ctx)

		if contextAddUse || cfg.CurrentContext == "" {
			cfg.CurrentContext = contextAddName
		}

		if err := SaveConfig(cfg); err != nil {
			return err
		}

		action := "Added"
		if existing {
			action = "Updated"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s context %q\n", action, contextAddName)
		if cfg.CurrentContext == contextAddName {
			fmt.Fprintf(cmd.OutOrStdout(), "Switched to context %q\n", contextAddName)
		}
		return nil
	},
}

func EnsureContextToken(cmd *cobra.Command, ctx *MCContext, status io.Writer) error {
	if ctx == nil || ctx.Server == "" || ctx.AccessToken() != "" {
		return nil
	}
	if ctx.OIDC != nil && ctx.OIDC.RefreshToken != "" {
		if token, err := resolveContextToken(ctx); err == nil && token != "" {
			return nil
		}
	}

	var lastErr error
	for _, loginServer := range oidcLoginServerCandidates(ctx.Server) {
		fmt.Fprintf(status, "No token configured for context %q; starting OIDC login for %s\n", ctx.Name, loginServer)
		tokens, endpoints, err := oidcLogin(cmd, loginServer, status)
		if err == nil {
			ctx.SetOIDCTokens(tokens)
			ctx.Endpoints = endpoints
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("OAuth login failed for %s: %w", ctx.Server, lastErr)
}

func oidcLoginServerCandidates(serverURL string) []string {
	serverURL = strings.TrimRight(serverURL, "/")
	if strings.HasSuffix(serverURL, "/api") {
		return uniqueStrings([]string{strings.TrimSuffix(serverURL, "/api"), serverURL})
	}
	return uniqueStrings([]string{serverURL})
}

// ResolveAPIBase probes both the frontend proxy API path and the direct backend
// path, returning the base URL that serves Mission Control's unauthenticated
// /health endpoint.
func ResolveAPIBase(serverURL string) (string, error) {
	serverURL = strings.TrimRight(serverURL, "/")
	if serverURL == "" {
		return "", fmt.Errorf("server URL is required")
	}

	var failures []string
	for _, candidate := range apiBaseCandidates(serverURL) {
		ok, err := probeAPIHealth(gocontext.Background(), candidate)
		if err == nil && ok {
			return candidate, nil
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate, err))
		} else {
			failures = append(failures, fmt.Sprintf("%s: /health did not return OK", candidate))
		}
	}
	return "", fmt.Errorf("could not find Mission Control API for %s (%s)", serverURL, strings.Join(failures, "; "))
}

func apiBaseCandidates(serverURL string) []string {
	serverURL = strings.TrimRight(serverURL, "/")
	if strings.HasSuffix(serverURL, "/api") {
		return uniqueStrings([]string{serverURL, strings.TrimSuffix(serverURL, "/api")})
	}
	return uniqueStrings([]string{serverURL + "/api", serverURL})
}

func probeAPIHealth(ctx gocontext.Context, baseURL string) (bool, error) {
	healthURL, err := url.JoinPath(baseURL, "health")
	if err != nil {
		return false, err
	}
	ctx, cancel := gocontext.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(body)) == "OK", nil
}

func init() {
	contextAddCmd.Flags().StringVar(&contextAddName, "name", "", "Context name (required)")
	contextAddCmd.Flags().StringVar(&contextAddServer, "server", "", "Mission Control server URL")
	contextAddCmd.Flags().StringVar(&contextAddDB, "db-url", "", "Direct database connection URL")
	contextAddCmd.Flags().StringVar(&contextAddToken, "token", "", "API token for the server")
	contextAddCmd.Flags().BoolVar(&contextAddUse, "use", false, "Switch to this context after adding")

	ContextCmd.AddCommand(contextUseCmd, contextListCmd, contextCurrentCmd, contextAddCmd, contextRemoveCmd)
}
