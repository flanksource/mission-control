//go:build faro

package clientcmd

import "github.com/spf13/cobra"

func addConnectionDryRunFlag(_ *cobra.Command, _ *ConnectionFlags) {}

func runConnectionDryRun(_ *ConnectionFlags) (bool, error) { return false, nil }
