package clientcmd

import "github.com/spf13/cobra"

// RegisterClientCommandOption customizes which client command surfaces are
// attached to a root command.
type RegisterClientCommandOption func(*registerClientCommandOptions)

type registerClientCommandOptions struct {
	registerCachedPluginCommands bool
	contextScopedPluginCache     bool
}

// WithContextScopedPluginCache makes dynamic plugin commands use faro's
// per-context cache instead of the process-wide local/plugin-supervisor cache.
func WithContextScopedPluginCache() RegisterClientCommandOption {
	return func(opts *registerClientCommandOptions) {
		opts.registerCachedPluginCommands = false
		opts.contextScopedPluginCache = true
	}
}

// AuthCmd is the parent for authentication subcommands. The client owns `auth
// login`; the server binary attaches its own token/check/password-reset
// subcommands to this same parent.
var AuthCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authentication commands",
}

func init() {
	AuthCmd.AddCommand(AuthLoginCmd)
}

// RegisterClientCommands attaches all remote-client command surfaces to root.
// Both the full mission-control binary and the slim faro binary call this.
func RegisterClientCommands(root *cobra.Command, options ...RegisterClientCommandOption) {
	opts := registerClientCommandOptions{registerCachedPluginCommands: true}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}

	root.PersistentFlags().StringVar(&contextFlag, "context", "", "Mission Control context to use")
	root.AddCommand(AuthCmd, ContextCmd, WhoamiCmd, Playbook, Connection, PluginCmd)

	pluginHostRoot = root
	pluginCacheContextScoped = opts.contextScopedPluginCache
	registerPluginHARFlag(root)
	if opts.registerCachedPluginCommands {
		_ = registerCachedPluginCommands(PluginCmd, root)
	}
}
