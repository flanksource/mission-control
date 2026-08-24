package clientcmd

import (
	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/clientcmd/mccontext"
)

// registerRetryFlags binds --retries and --retry-delay straight onto the client
// policy. Called once at init() time, alongside the other global client flags.
// The policy itself lives in mccontext, which owns client construction and is
// read before cobra parses anything.
func registerRetryFlags(root *cobra.Command) {
	root.PersistentFlags().IntVar(&mccontext.RetryAttempts, "retries", mccontext.RetryAttempts,
		"Number of additional attempts for a request that fails transiently; 0 disables retry")
	root.PersistentFlags().DurationVar(&mccontext.RetryDelay, "retry-delay", mccontext.RetryDelay,
		"Fixed delay between retry attempts")
}
