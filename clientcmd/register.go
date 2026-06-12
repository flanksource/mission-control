package clientcmd

import "github.com/spf13/cobra"

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
func RegisterClientCommands(root *cobra.Command) {
	root.PersistentFlags().StringVar(&contextFlag, "context", "", "Mission Control context to use")
	root.AddCommand(AuthCmd, ContextCmd, WhoamiCmd, Playbook, Connection, PluginCmd)

	pluginHostRoot = root
	registerPluginHARFlag(root)
	_ = registerCachedPluginCommands(PluginCmd, root)
}
