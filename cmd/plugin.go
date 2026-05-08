package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	osExec "os/exec"
	"path/filepath"
	"strings"
	"time"

	dutyContext "github.com/flanksource/duty/context"
	goplugin "github.com/hashicorp/go-plugin"
	"github.com/spf13/cobra"

	icplugin "github.com/flanksource/incident-commander/plugin"
	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/registry"
)

// pluginParams accumulates --param k=v repeats.
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
		return fmt.Errorf("expected k=v, got %q", v)
	}
	p.values[v[:idx]] = v[idx+1:]
	return nil
}

func (p *pluginParams) Type() string { return "key=value" }

var (
	pluginConfigID string
	pluginRawJSON  bool
	pluginParamSet pluginParams
)

// PluginCmd is the parent for plugin operations: `mission-control plugin <name> <op>`.
var PluginCmd = &cobra.Command{
	Use:   "plugin <name> <operation>",
	Short: "Invoke an operation exposed by a Mission Control plugin",
	Long: `Spawn a plugin binary (discovered on $MISSION_CONTROL_PLUGIN_PATH) and
invoke one of its operations directly. Mission-control does not need to be
running — the CLI talks to the plugin via the same gRPC contract used by the
host process.`,
	Args: cobra.MinimumNArgs(2),
	RunE: runPluginOp,
}

func init() {
	PluginCmd.Flags().StringVar(&pluginConfigID, "config-id", "", "Catalog/config item id passed to the operation")
	PluginCmd.Flags().BoolVar(&pluginRawJSON, "json", false, "Emit raw application/clicky+json instead of pretty-printing")
	PluginCmd.Flags().Var(&pluginParamSet, "param", "Key=value parameters (repeatable)")
	Root.AddCommand(PluginCmd)
}

func runPluginOp(_ *cobra.Command, args []string) error {
	name, op := args[0], args[1]

	binPath, err := findPluginBinary(name)
	if err != nil {
		return err
	}

	cli, pluginCli, err := dialPlugin(binPath)
	if err != nil {
		return err
	}
	defer cli.Kill()

	manifestCtx, manifestCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer manifestCancel()

	if _, err := pluginCli.Service.RegisterPlugin(manifestCtx, &pluginpb.RegisterRequest{
		HostProtocolVersion: uint32(icplugin.ProtocolVersion),
	}); err != nil {
		return fmt.Errorf("plugin RegisterPlugin: %w", err)
	}

	params, err := json.Marshal(pluginParamSet.values)
	if err != nil {
		return err
	}

	invokeCtx, invokeCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer invokeCancel()

	resp, err := pluginCli.Service.Invoke(invokeCtx, &pluginpb.InvokeRequest{
		Operation:    op,
		ParamsJson:   params,
		ConfigItemId: pluginConfigID,
	})
	if err != nil {
		return fmt.Errorf("invoke %s/%s: %w", name, op, err)
	}
	if resp.ErrorMessage != "" {
		return fmt.Errorf("plugin error: %s (%s)", resp.ErrorMessage, resp.ErrorCode)
	}

	if pluginRawJSON {
		_, err := os.Stdout.Write(resp.Result)
		return err
	}

	// Best-effort pretty-print: pass JSON through indenter. Real clicky
	// rendering happens automatically when the embedded UI consumes
	// application/clicky+json over HTTP — for the CLI we settle for
	// human-readable JSON until we wire the clicky CLI renderer.
	var pretty any
	if err := json.Unmarshal(resp.Result, &pretty); err == nil {
		out, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	_, err = os.Stdout.Write(resp.Result)
	return err
}

// findPluginBinary scans MISSION_CONTROL_PLUGIN_PATH for a binary matching
// `name`. If multiple binaries match (e.g. name and name-darwin-arm64) it
// prefers the exact match.
func findPluginBinary(name string) (string, error) {
	dir := registry.PluginPath()
	exact := filepath.Join(dir, name)
	if info, err := os.Stat(exact); err == nil && !info.IsDir() {
		return exact, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("scan %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), name) {
			return filepath.Join(dir, e.Name()), nil
		}
	}
	return "", fmt.Errorf("plugin %q not found in %s", name, dir)
}

func dialPlugin(binPath string) (*goplugin.Client, *icplugin.Client, error) {
	cmd := osExec.Command(binPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("%s=%s", icplugin.Handshake.MagicCookieKey, icplugin.Handshake.MagicCookieValue),
	)
	cli := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  icplugin.Handshake,
		Plugins:          icplugin.PluginMap,
		Cmd:              cmd,
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Managed:          true,
	})

	rpcClient, err := cli.Client()
	if err != nil {
		cli.Kill()
		return nil, nil, fmt.Errorf("plugin rpc: %w", err)
	}
	raw, err := rpcClient.Dispense(icplugin.PluginName)
	if err != nil {
		cli.Kill()
		return nil, nil, fmt.Errorf("plugin dispense: %w", err)
	}
	pluginCli, ok := raw.(*icplugin.Client)
	if !ok {
		cli.Kill()
		return nil, nil, fmt.Errorf("plugin: unexpected dispense type %T", raw)
	}
	return cli, pluginCli, nil
}

// _ keeps duty/context imported — useful when the CLI later needs to share
// resolution helpers with the server (e.g. local connection lookup).
var _ dutyContext.Context
