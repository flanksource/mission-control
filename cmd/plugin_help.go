package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/clicky/rpc"
	"github.com/spf13/cobra"
)

type operationDispatcher func(cmd *cobra.Command, plugin, op string, params map[string]string, configID string, raw bool) error

func newOperationCommandWithDispatcher(plugin string, op rpc.RPCOperation, dispatcher operationDispatcher) *cobra.Command {
	var (
		params   pluginParams
		configID string
		raw      bool
	)
	cmd := &cobra.Command{
		Use:          op.Name,
		Short:        op.Description,
		Long:         formatOperationLong(op),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := validateOperationInputs(op, params.values, configID); err != nil {
				return err
			}
			return dispatcher(c, plugin, op.Name, params.values, configID, raw)
		},
	}
	cmd.Flags().Var(&params, "param", "Operation parameter (repeatable, key=value)")
	cmd.Flags().StringVar(&configID, "config-id", "", "Catalog config item id passed to the operation")
	cmd.Flags().BoolVar(&raw, "json", false, "Emit raw JSON instead of pretty-printing")
	return cmd
}

func validateOperationInputs(op rpc.RPCOperation, params map[string]string, configID string) error {
	if operationRequiresConfigID(op) && strings.TrimSpace(configID) == "" {
		return fmt.Errorf("--config-id is required for config-scoped operation %q", op.Name)
	}
	missing := make([]string, 0)
	for _, param := range op.Parameters {
		if !param.Required {
			continue
		}
		if strings.TrimSpace(params[param.Name]) == "" {
			missing = append(missing, param.Name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("missing required parameter(s): %s; pass with --param key=value", strings.Join(missing, ", "))
	}
	return nil
}

func operationRequiresConfigID(op rpc.RPCOperation) bool {
	for _, tag := range op.Tags {
		if tag == "config" {
			return true
		}
	}
	return false
}

// formatOperationLong renders the per-op help: description, then a table
// of parameters (Required, Default, Description). Parameters come from
// the cached schema; an empty list means the operation accepts free-form
// `--param k=v` only.
func formatOperationLong(op rpc.RPCOperation) string {
	var b strings.Builder
	if op.Description != "" {
		b.WriteString(op.Description)
		b.WriteString("\n")
	}
	if len(op.Tags) > 0 {
		fmt.Fprintf(&b, "Scope: %s\n", strings.Join(op.Tags, ", "))
	}
	if len(op.Parameters) == 0 {
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString("\nParameters (pass via --param key=value):\n")
	params := append([]rpc.RPCParameter(nil), op.Parameters...)
	sort.SliceStable(params, func(i, j int) bool { return params[i].Name < params[j].Name })
	for _, p := range params {
		marker := " "
		if p.Required {
			marker = "*"
		}
		typ := p.Type
		if typ == "" {
			typ = "string"
		}
		line := fmt.Sprintf("  %s %s (%s)", marker, p.Name, typ)
		if p.Default != nil {
			line += fmt.Sprintf(" [default: %v]", p.Default)
		}
		b.WriteString(line)
		if p.Description != "" {
			b.WriteString("\n      ")
			b.WriteString(p.Description)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n* = required")
	return strings.TrimRight(b.String(), "\n")
}
