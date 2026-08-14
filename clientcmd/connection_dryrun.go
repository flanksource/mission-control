//go:build !faro

package clientcmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func addConnectionDryRunFlag(cmd *cobra.Command, flags *ConnectionFlags) {
	cmd.Flags().BoolVar(&flags.DryRun, "dry-run", false, "Output Kubernetes YAML instead of saving to database")
}

func runConnectionDryRun(flags *ConnectionFlags) (bool, error) {
	if !flags.DryRun {
		return false, nil
	}
	out, err := marshalDryRunOutput(flags)
	if err != nil {
		return true, fmt.Errorf("failed to marshal dry-run output: %w", err)
	}
	fmt.Print(string(out))
	return true, nil
}
