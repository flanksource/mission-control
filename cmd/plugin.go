package cmd

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/plugin/manifestcache"
)

// pluginParams accumulates --param k=v repeats. Used by the dynamic
// per-operation subcommands and by the legacy `plugin <name> <op>` form.
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
		return fmt.Errorf("expected k=v, got %q", v)
	}
	p.values[v[:idx]] = v[idx+1:]
	return nil
}

func (p *pluginParams) Type() string { return "key=value" }

// pluginGroupID is the cobra group used to visually separate per-plugin
// commands from built-in commands in --help output.
const pluginGroupID = "plugins"

// PluginCmd is the parent for plugin operations under
// `mission-control plugin <name> ...`. Each cached plugin is also added as
// a top-level subcommand of Root, so `mission-control <name> <op>` works.
var PluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Inspect and invoke Mission Control plugins",
	Long: `Manage and invoke Mission Control plugin operations.

Cached plugin schemas live under the user cache directory and are populated:

  - automatically when the host server registers the plugin, or
  - on demand via 'mission-control plugin <name> refresh-cache', which
    in API mode fetches schemas from the configured server and in DB mode
    spawns the local plugin binary once.

Operations dispatch differently per active context:

  - API mode (server + token): forwards over HTTP, asks for clicky+json,
    renders the response.
  - DB / local mode: spawns the local plugin binary on each invocation.`,
}

// pluginListCmd prints what's in the cache. Useful for debugging which
// mode a CLI invocation will use.
var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List plugins with cached schemas",
	RunE: func(cmd *cobra.Command, _ []string) error {
		entries, err := manifestcache.List()
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No plugins cached. Run 'mission-control plugin refresh-cache' to populate.")
			return nil
		}
		for _, e := range entries {
			origin := string(e.Source)
			if e.Source == manifestcache.SourceRemoteServer && e.ServerURL != "" {
				origin = origin + " " + e.ServerURL
			} else if e.Source == manifestcache.SourceLocalBinary && e.BinaryPath != "" {
				origin = origin + " " + e.BinaryPath
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-12s %2d ops  %s\n",
				e.Service.Name, e.Service.Version, len(e.Service.Operations), origin)
		}
		return nil
	},
}

// pluginRefreshCacheCmd refreshes one or all plugin caches. Without args it
// refreshes everything reachable in the active mode.
var pluginRefreshCacheCmd = &cobra.Command{
	Use:   "refresh-cache [name]",
	Short: "Re-fetch plugin schemas from the active context",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var only string
		if len(args) == 1 {
			only = args[0]
		}
		mode, mcCtx, err := resolveMode()
		if err != nil {
			return err
		}
		switch mode {
		case modeAPI:
			names, err := refreshAllFromServer(cmd, mcCtx)
			if err != nil {
				return err
			}
			if only != "" && !slices.Contains(names, only) {
				return fmt.Errorf("plugin %q not exposed by %s", only, mcCtx.Server)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Refreshed %d plugin(s) from %s\n", len(names), mcCtx.Server)
			return nil
		case modeLocal:
			if only == "" {
				return errors.New("DB / local mode requires a plugin name; usage: refresh-cache <name>")
			}
			entry, err := refreshOneFromBinary(cmd, only)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Refreshed %s (%d ops) from %s\n",
				entry.Service.Name, len(entry.Service.Operations), entry.BinaryPath)
			return nil
		}
		return fmt.Errorf("unable to determine mode: no API context and no $MISSION_CONTROL_PLUGIN_PATH plugins")
	},
}

func init() {
	Root.AddGroup(&cobra.Group{ID: pluginGroupID, Title: "Plugins:"})
	PluginCmd.AddGroup(&cobra.Group{ID: pluginGroupID, Title: "Plugins:"})

	registerPluginHARFlag(Root)
	PluginCmd.AddCommand(pluginListCmd, pluginRefreshCacheCmd)
	registerCachedPluginCommands()
	Root.AddCommand(PluginCmd)
}

// registerCachedPluginCommands reads every cached entry and attaches a
// per-plugin command tree to both PluginCmd and Root. Called once at init().
// Empty cache → no per-plugin commands; users still get
// `mission-control plugin {list,refresh-cache}`.
func registerCachedPluginCommands() {
	entries, err := manifestcache.List()
	if err != nil {
		return
	}
	for _, e := range entries {
		nested, top := buildPluginCommands(*e)
		nested.GroupID = pluginGroupID
		top.GroupID = pluginGroupID
		PluginCmd.AddCommand(nested)
		Root.AddCommand(top)
	}
}
