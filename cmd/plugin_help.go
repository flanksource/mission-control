package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/clicky/rpc"
	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/plugin/manifestcache"
)

// buildPluginCommands returns two cobra command trees built from a cached
// manifestcache entry: a "nested" tree to attach under PluginCmd
// (`mission-control plugin <name> ...`) and a "top" tree to attach under
// Root (`mission-control <name> ...`). Both trees expose one subcommand
// per cached operation; their RunE delegates to dispatchOperation.
func buildPluginCommands(entry manifestcache.Entry) (nested, top *cobra.Command) {
	nested = newPluginRoot(entry, false)
	top = newPluginRoot(entry, true)
	return nested, top
}

// newPluginRoot builds the per-plugin parent command. topLevel toggles the
// short-form usage shown in --help (whether the user typed `plugin <name>`
// or just `<name>`).
func newPluginRoot(entry manifestcache.Entry, topLevel bool) *cobra.Command {
	use := entry.Service.Name
	if !topLevel {
		use = entry.Service.Name
	}
	short := entry.Service.Description
	if short == "" {
		short = fmt.Sprintf("Operations for the %q plugin", entry.Service.Name)
	}
	root := &cobra.Command{
		Use:          use,
		Short:        short,
		Long:         formatPluginLong(entry, topLevel),
		SilenceUsage: true,
	}
	for _, op := range entry.Service.Operations {
		root.AddCommand(newOperationCommand(entry.Service.Name, op))
	}
	return root
}

// newOperationCommand builds a single operation subcommand. The RunE wires
// in the dispatcher; flags are limited to the always-applicable
// `--param k=v`, `--config-id`, `--json`. Per-operation parameters
// declared in the manifest are surfaced in Long: text only — they map to
// `--param <name>=<value>`.
func newOperationCommand(plugin string, op rpc.RPCOperation) *cobra.Command {
	return newOperationCommandWithDispatcher(plugin, op, dispatchOperation)
}

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

// formatPluginLong builds the description shown above the subcommand list
// for `mission-control <plugin> --help`. Includes the cache provenance so
// users can tell whether help is coming from a server or a local binary.
func formatPluginLong(entry manifestcache.Entry, topLevel bool) string {
	var b strings.Builder
	if entry.Service.Description != "" {
		b.WriteString(entry.Service.Description)
		b.WriteString("\n\n")
	}
	if entry.Service.Version != "" {
		fmt.Fprintf(&b, "Version: %s\n", entry.Service.Version)
	}
	switch entry.Source {
	case manifestcache.SourceRemoteServer:
		fmt.Fprintf(&b, "Source:  remote server (%s)\n", entry.ServerURL)
	case manifestcache.SourceLocalBinary:
		fmt.Fprintf(&b, "Source:  local binary (%s)\n", entry.BinaryPath)
	}
	if !entry.CachedAt.IsZero() {
		fmt.Fprintf(&b, "Cached:  %s\n", entry.CachedAt.Format("2006-01-02 15:04:05 MST"))
	}
	if !topLevel {
		b.WriteString("\nThis is the nested form. The same operations are also reachable as `mission-control ")
		b.WriteString(entry.Service.Name)
		b.WriteString(" <op>`.\n")
	}
	return strings.TrimRight(b.String(), "\n")
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
