package clientcmd

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/sdk"
)

// Defaults are set here rather than only in the flag registration because the plugin cache is
// populated before cobra parses anything, and it would otherwise read a zero policy.
var (
	retriesFlag    = 3
	retryDelayFlag = time.Second
)

// registerRetryFlags attaches --retries and --retry-delay to the given root command. Called once
// at init() time, alongside the other global client flags.
func registerRetryFlags(root *cobra.Command) {
	root.PersistentFlags().IntVar(&retriesFlag, "retries", retriesFlag,
		"Number of additional attempts for a request that fails transiently; 0 disables retry")
	root.PersistentFlags().DurationVar(&retryDelayFlag, "retry-delay", retryDelayFlag,
		"Fixed delay between retry attempts")
}

// retryOption is the one place the CLI's flags become a client policy.
func retryOption() sdk.ClientOption {
	return sdk.WithRetry(retriesFlag, retryDelayFlag)
}
