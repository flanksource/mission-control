package clientcmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/clicky/rpc"
	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/plugin/manifestcache"
)

func registerCachedPluginCommands(pluginRoot, root *cobra.Command) error {
	entries, err := manifestcache.List()
	if err != nil {
		return err
	}
	return registerCachedPluginCommandEntries(pluginRoot, root, entries)
}

func registerCachedPluginCommandsFromDir(pluginRoot, root *cobra.Command, dir string) error {
	entries, err := manifestcache.ListFromDir(dir)
	if err != nil {
		return err
	}
	return registerCachedPluginCommandEntries(pluginRoot, root, entries)
}

func registerCachedPluginCommandEntries(pluginRoot, root *cobra.Command, entries []*manifestcache.Entry) error {
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Service.Name < entries[j].Service.Name
	})
	for _, entry := range entries {
		if entry == nil || entry.Service.Name == "" {
			continue
		}
		nested, top := buildPluginCommands(*entry)
		if pluginRoot != nil && !commandExists(pluginRoot, nested.Name()) {
			pluginRoot.AddCommand(nested)
		}
		if root != nil && !commandExists(root, top.Name()) {
			root.AddCommand(top)
		}
	}
	return nil
}

func commandExists(parent *cobra.Command, name string) bool {
	if parent == nil || name == "" {
		return false
	}
	for _, cmd := range parent.Commands() {
		if cmd.Name() == name {
			return true
		}
	}
	return false
}

func buildPluginCommands(entry manifestcache.Entry) (nested, top *cobra.Command) {
	return newPluginRoot(entry, false), newPluginRoot(entry, true)
}

func newPluginRoot(entry manifestcache.Entry, topLevel bool) *cobra.Command {
	short := entry.Service.Description
	if short == "" {
		short = fmt.Sprintf("Operations for the %q plugin", entry.Service.Name)
	}
	var (
		params   pluginParams
		configID string
		raw      bool
	)
	root := &cobra.Command{
		Use:          entry.Service.Name + " <operation>",
		Short:        short,
		Long:         formatPluginLong(entry, topLevel),
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return dispatchOperation(cmd, entry.Service.Name, args[0], params.values, configID, raw)
		},
	}
	root.Flags().Var(&params, "param", "Operation parameter (repeatable, key=value)")
	root.Flags().StringVar(&configID, "config-id", "", "Catalog config item id passed to the operation")
	root.Flags().BoolVar(&raw, "json", false, "Emit raw JSON instead of pretty-printing")
	for _, op := range entry.Service.Operations {
		root.AddCommand(newOperationCommand(entry.Service.Name, op))
	}
	return root
}

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
		fmt.Fprintf(&b, "Source: remote server (%s)\n", entry.ServerURL)
	case manifestcache.SourceLocalBinary:
		fmt.Fprintf(&b, "Source: local binary (%s)\n", entry.BinaryPath)
	}
	if !entry.CachedAt.IsZero() {
		fmt.Fprintf(&b, "Cached: %s\n", entry.CachedAt.Format("2006-01-02 15:04:05 MST"))
	}
	if !topLevel {
		fmt.Fprintf(&b, "\nThe same operations are also reachable as `incident-commander %s <operation>`.\n", entry.Service.Name)
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
