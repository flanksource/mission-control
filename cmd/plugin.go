package cmd

import (
	gocontext "context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/plugin/machinery/local"
	"github.com/flanksource/incident-commander/plugin/manifestcache"
)

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
	registerPluginHARFlag(Root)
	Root.AddCommand(PluginCmd)
	_ = registerCachedPluginCommands(PluginCmd, Root)
}

func runPluginOp(cmd *cobra.Command, args []string) error {
	return dispatchOperation(cmd, args[0], args[1], pluginOpts.Params.values, pluginOpts.ConfigID, pluginOpts.RawJSON)
}

func runPluginRefreshCache(cmd *cobra.Command, args []string) error {
	mode, mc, err := resolveMode()
	if err != nil {
		return err
	}

	var names []string
	if len(args) == 0 {
		if mode != modeAPI {
			return fmt.Errorf("plugin name is required when refreshing from local binaries")
		}
		names, err = refreshPluginCacheFromServer(cmd, mc)
	} else {
		name := args[0]
		switch mode {
		case modeAPI:
			names, err = refreshPluginCacheFromServer(cmd, mc)
			if err == nil && !containsString(names, name) {
				return fmt.Errorf("plugin %q was not returned by %s", name, mc.Server)
			}
		case modeLocal:
			var entry *manifestcache.Entry
			entry, err = refreshPluginCacheFromBinary(cmd, name)
			if entry != nil && entry.Service.Name != "" {
				names = []string{entry.Service.Name}
			} else {
				names = []string{name}
			}
		default:
			return fmt.Errorf("unable to determine dispatch mode")
		}
	}
	if err != nil {
		return err
	}

	_ = registerCachedPluginCommands(PluginCmd, Root)
	sort.Strings(names)
	fmt.Fprintf(cmd.OutOrStdout(), "Refreshed plugin command cache: %s\n", strings.Join(names, ", "))
	return nil
}

func refreshPluginCacheFromServer(cmd *cobra.Command, mc *MCContext) ([]string, error) {
	if mc == nil || mc.Server == "" {
		return nil, fmt.Errorf("no Mission Control server configured")
	}
	token, err := resolveContextToken(mc)
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

func refreshPluginCacheFromBinary(cmd *cobra.Command, name string) (*manifestcache.Entry, error) {
	ctx, cancel := gocontext.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	return manifestcache.PopulateLocal(ctx, name, manifestcache.PopulateOptions{
		BinaryDir: local.PluginPath(),
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
