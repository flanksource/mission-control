package clientcmd

import (
	gocontext "context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/plugin/manifestcache"
)

const contextCacheTTL = 24 * time.Hour

// ContextCacheResult describes a context-scoped plugin cache refresh/check.
type ContextCacheResult struct {
	ContextName string
	CacheDir    string
	Plugins     []string
	Refreshed   bool
}

// PreselectContextFromArgs extracts --context from raw argv before cobra parses
// flags. Dynamic plugin commands are registered before Execute(), so CLIs need
// this to select the correct per-context cache for `--context X --help`.
func PreselectContextFromArgs(args []string) string {
	for i := range args {
		arg := args[i]
		if arg == "--" {
			break
		}

		if arg == "--context" && i+1 < len(args) {
			contextFlag = args[i+1]
			return contextFlag
		}

		if after, ok := strings.CutPrefix(arg, "--context="); ok {
			contextFlag = after
			return contextFlag
		}
	}

	return contextFlag
}

func contextCacheBaseDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		panic(fmt.Errorf("failed to get user cache dir"))
	}

	return filepath.Join(base, "mission-control")
}

func safeContextName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		panic(fmt.Errorf("context name is empty"))
	}

	return b.String()
}

// ContextCacheDir returns a cache directory scoped only by Mission Control context name.
func ContextCacheDir(contextName string) string {
	return filepath.Join(contextCacheBaseDir(), "context-"+safeContextName(contextName))
}

func contextCacheDir(mc *MCContext) string {
	return ContextCacheDir(mc.Name)
}

func contextPluginCacheDir(mc *MCContext) string {
	return filepath.Join(contextCacheDir(mc), "plugins")
}

// CurrentContextPluginCacheDir returns the plugin cache directory scoped to the
// current Mission Control context name.
func CurrentContextPluginCacheDir() (string, error) {
	mc, err := currentMCContext()
	if err != nil {
		return "", err
	}
	if mc == nil {
		return "", fmt.Errorf("no Mission Control context configured")
	}
	return contextPluginCacheDir(mc), nil
}

func contextLastRanPath(mc *MCContext) string {
	return filepath.Join(contextCacheDir(mc), "last-ran.txt")
}

func currentMCContext() (*MCContext, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	mc := cfg.CurrentMCContext()
	if mc == nil {
		return nil, nil
	}

	return mc, nil
}

func shouldRefreshContextCache(mc *MCContext, now time.Time) bool {
	data, err := os.ReadFile(contextLastRanPath(mc))
	if err != nil {
		return true
	}
	lastRan, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return true
	}
	return !now.Before(lastRan.Add(contextCacheTTL))
}

func writeContextLastRan(mc *MCContext, now time.Time) error {
	path := contextLastRanPath(mc)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(now.Format(time.RFC3339)+"\n"), 0o600)
}

// EnsureCurrentContextCache refreshes the current per-context cache when it has
// not been fetched before, or when the last successful fetch is older than 24h.
func EnsureCurrentContextCache(ctx gocontext.Context) (*ContextCacheResult, error) {
	return refreshCurrentContextCache(ctx, false)
}

// RebuildCurrentContextCache clears and rebuilds the current per-context cache.
func RebuildCurrentContextCache(ctx gocontext.Context) (*ContextCacheResult, error) {
	return refreshCurrentContextCache(ctx, true)
}

// SetupContextCachedPluginCommands selects the requested context from raw argv,
// refreshes that context's plugin cache when needed, and registers cached plugin
// commands. Refresh and registration errors are returned separately so callers
// can log refresh failures while still using an existing cache.
func SetupContextCachedPluginCommands(ctx gocontext.Context, root *cobra.Command, args []string) (refreshErr, registerErr error) {
	PreselectContextFromArgs(args)
	if !IsRefreshCacheCommand(args) {
		_, refreshErr = EnsureCurrentContextCache(ctx)
	}
	registerErr = RegisterContextCachedPluginCommands(root)
	return refreshErr, registerErr
}

func refreshCurrentContextCache(ctx gocontext.Context, force bool) (*ContextCacheResult, error) {
	mc, err := currentMCContext()
	if err != nil {
		return nil, err
	}
	if mc == nil || mc.Server == "" {
		return nil, nil
	}

	result := &ContextCacheResult{
		ContextName: mc.Name,
		CacheDir:    contextCacheDir(mc),
	}

	now := time.Now()
	if !force && !shouldRefreshContextCache(mc, now) {
		return result, nil
	}

	token, err := ResolveContextToken(mc)
	if err != nil {
		return result, err
	}

	names, err := manifestcache.PopulateAPI(ctx, manifestcache.PopulateOptions{
		Server:        mc.Server,
		Token:         token,
		CacheDir:      contextPluginCacheDir(mc),
		ClearExisting: true,
	})
	if err != nil {
		return result, err
	}
	if err := writeContextLastRan(mc, now); err != nil {
		return result, err
	}
	result.Plugins = names
	result.Refreshed = true
	return result, nil
}

// RegisterContextCachedPluginCommands attaches plugin commands from the current
// per-context plugin cache. Missing caches are not an error.
func RegisterContextCachedPluginCommands(root *cobra.Command) error {
	mc, err := currentMCContext()
	if err != nil {
		return err
	}
	if mc == nil {
		return nil
	}
	return registerCachedPluginCommandsFromDir(PluginCmd, root, contextPluginCacheDir(mc))
}

// IsRefreshCacheCommand reports whether argv targets faro's root refresh-cache
// command, so startup can avoid doing an automatic refresh immediately before a
// forced refresh.
func IsRefreshCacheCommand(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return false
		}
		if strings.HasPrefix(arg, "-") {
			switch arg {
			case "--context", "--har", "--log-level":
				if i+1 < len(args) {
					i++
				}
			}
			continue
		}
		return arg == "refresh-cache"
	}
	return false
}
