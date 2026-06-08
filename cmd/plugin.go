package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// pluginParams accumulates repeated --param key=value flags.
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
		return fmt.Errorf("expected key=value, got %q", v)
	}
	p.values[v[:idx]] = v[idx+1:]
	return nil
}

func (p *pluginParams) Type() string { return "key=value" }

type pluginOptions struct {
	ConfigID string
	RawJSON  bool
	Params   pluginParams
}

var pluginOpts pluginOptions

// PluginCmd invokes operations exposed by plugins running in Mission Control.
var PluginCmd = &cobra.Command{
	Use:               "plugin <name> <operation>",
	Short:             "Invoke an operation exposed by a Mission Control plugin",
	Long:              "Invoke an operation exposed by a plugin through the running Mission Control HTTP API. Uses the current CLI context for the server. Auth uses the context token, or PLUGIN_SERVER_AUTH for basic auth when set.",
	Args:              cobra.ExactArgs(2),
	SilenceUsage:      true,
	DisableAutoGenTag: true,
	RunE:              runPluginOp,
}

func init() {
	PluginCmd.Flags().StringVar(&pluginOpts.ConfigID, "config-id", "", "Catalog/config item id passed to the operation")
	PluginCmd.Flags().BoolVar(&pluginOpts.RawJSON, "json", false, "Emit raw response instead of pretty-printing JSON")
	PluginCmd.Flags().Var(&pluginOpts.Params, "param", "Key=value parameters (repeatable)")
	registerPluginHARFlag(Root)
	Root.AddCommand(PluginCmd)
}

func runPluginOp(cmd *cobra.Command, args []string) error {
	return dispatchOperation(cmd, args[0], args[1], pluginOpts.Params.values, pluginOpts.ConfigID, pluginOpts.RawJSON)
}
