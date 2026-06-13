package clientcmd

import (
	gocontext "context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/plugin/manifestcache"
)

// LocalPluginRefresh, when set by the full mission-control binary, refreshes
// the plugin command cache from a locally-installed plugin binary. The slim
// faro client leaves it nil and refreshes exclusively from the server.
var LocalPluginRefresh func(cmd *cobra.Command, args []string) ([]string, error)

// pluginHostRoot is the root command that cached plugin commands are attached
// to (set by RegisterClientCommands), so refresh-cache can re-register them.
var pluginHostRoot *cobra.Command

var pluginCacheContextScoped bool

// pluginParams accumulates repeated --param key=value flags.
type pluginParams struct {
	values map[string]string
}

func (p *pluginParams) String() string {
	parts := make([]string, 0, len(p.values))
	for k, v := range p.values {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}

func (p *pluginParams) Set(v string) error {
	if p.values == nil {
		p.values = map[string]string{}
	}
	idx := strings.IndexByte(v, '=')
	if idx <= 0 {
		return fmt.Errorf("expected key=value, got %q", v)
	}
	p.values[v[:idx]] = v[idx+1:]
	return nil
}

func (p *pluginParams) Type() string { return "key=value" }

type pluginOptions struct {
	ConfigID string
	RawJSON  bool
	Params   pluginParams
}

var pluginOpts pluginOptions

// PluginCmd invokes operations exposed by plugins running in Mission Control.
var PluginCmd = &cobra.Command{
	Use:               "plugin <name> <operation>",
	Short:             "Invoke an operation exposed by a Mission Control plugin",
	Long:              "Invoke an operation exposed by a plugin through the running Mission Control HTTP API. Uses the current CLI context for the server. Auth uses the context token, or PLUGIN_SERVER_AUTH for basic auth when set.",
	Args:              cobra.ExactArgs(2),
	SilenceUsage:      true,
	DisableAutoGenTag: true,
	RunE:              runPluginOp,
}

var pluginRefreshCacheCmd = &cobra.Command{
	Use:               "refresh-cache [plugin]",
	Short:             "Refresh cached plugin command metadata",
	Args:              cobra.MaximumNArgs(1),
	SilenceUsage:      true,
	DisableAutoGenTag: true,
	RunE:              runPluginRefreshCache,
}

func init() {
	PluginCmd.Flags().StringVar(&pluginOpts.ConfigID, "config-id", "", "Catalog/config item id passed to the operation")
	PluginCmd.Flags().BoolVar(&pluginOpts.RawJSON, "json", false, "Emit raw response instead of pretty-printing JSON")
	PluginCmd.Flags().Var(&pluginOpts.Params, "param", "Key=value parameters (repeatable)")
	PluginCmd.AddCommand(pluginRefreshCacheCmd)
}

func runPluginOp(cmd *cobra.Command, args []string) error {
	return dispatchOperation(cmd, args[0], args[1], pluginOpts.Params.values, pluginOpts.ConfigID, pluginOpts.RawJSON)
}

func runPluginRefreshCache(cmd *cobra.Command, args []string) error {
	var names []string
	var err error

	if mc, ok := ContextHasAPI(); ok {
		if pluginCacheContextScoped {
			ctx, cancel := gocontext.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			var result *ContextCacheResult
			result, err = RebuildCurrentContextCache(ctx)
			if result != nil {
				names = result.Plugins
			}
		} else {
			names, err = refreshPluginCacheFromServer(cmd, mc)
		}
		if err == nil && len(args) > 0 && !containsString(names, args[0]) {
			return fmt.Errorf("plugin %q was not returned by %s", args[0], mc.Server)
		}
	} else if LocalPluginRefresh != nil {
		if len(args) == 0 {
			return fmt.Errorf("plugin name is required when refreshing from local binaries")
		}
		names, err = LocalPluginRefresh(cmd, args)
	} else {
		return fmt.Errorf("no API context and no local plugin support; configure one with `auth login` or use the full mission-control binary")
	}
	if err != nil {
		return err
	}

	var registerErr error
	if pluginCacheContextScoped {
		registerErr = RegisterContextCachedPluginCommands(pluginHostRoot)
	} else {
		registerErr = registerCachedPluginCommands(PluginCmd, pluginHostRoot)
	}
	if registerErr != nil {
		return registerErr
	}
	sort.Strings(names)
	fmt.Fprintf(cmd.OutOrStdout(), "Refreshed plugin command cache: %s\n", strings.Join(names, ", "))
	return nil
}

func refreshPluginCacheFromServer(cmd *cobra.Command, mc *MCContext) ([]string, error) {
	if mc == nil || mc.Server == "" {
		return nil, fmt.Errorf("no Mission Control server configured")
	}
	token, err := ResolveContextToken(mc)
	if err != nil {
		return nil, err
	}
	ctx, cancel := gocontext.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	collector, flush := startHAR()
	defer func() {
		if err := flush(); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
		}
	}()
	return manifestcache.PopulateAPI(ctx, manifestcache.PopulateOptions{
		Server: mc.Server,
		Token:  token,
		HAR:    collector,
	})
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
