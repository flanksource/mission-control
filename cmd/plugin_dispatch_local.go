package cmd

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"os"
	osExec "os/exec"
	"time"

	goplugin "github.com/hashicorp/go-plugin"
	"github.com/spf13/cobra"

	icplugin "github.com/flanksource/incident-commander/plugin"
	"github.com/flanksource/incident-commander/plugin/manifestcache"
	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
)

// dispatchLocal spawns the plugin binary, completes the gRPC handshake,
// invokes the operation, and shuts the plugin down. Used when the CLI has
// no API context configured (DB-only / local dev).
func dispatchLocal(cmd *cobra.Command, plugin, op string, params map[string]string, configID string, raw bool) error {
	binPath, err := manifestcache.FindBinaryFor(plugin)
	if err != nil {
		return err
	}

	cli, pluginCli, err := dialPlugin(binPath)
	if err != nil {
		return err
	}
	defer cli.Kill()

	registerCtx, cancelRegister := gocontext.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancelRegister()
	if _, err := pluginCli.Service.RegisterPlugin(registerCtx, &pluginpb.RegisterRequest{
		HostProtocolVersion: uint32(icplugin.ProtocolVersion),
	}); err != nil {
		return fmt.Errorf("plugin RegisterPlugin: %w", err)
	}

	if params == nil {
		params = map[string]string{}
	}
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode params: %w", err)
	}

	invokeCtx, cancelInvoke := gocontext.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancelInvoke()
	resp, err := pluginCli.Service.Invoke(invokeCtx, &pluginpb.InvokeRequest{
		Operation:    op,
		ParamsJson:   body,
		ConfigItemId: configID,
	})
	if err != nil {
		return fmt.Errorf("invoke %s/%s: %w", plugin, op, err)
	}
	if resp.ErrorMessage != "" {
		return fmt.Errorf("plugin error: %s (%s)", resp.ErrorMessage, resp.ErrorCode)
	}

	return renderResult(cmd, resp.Result, raw)
}

// dialPlugin spawns binPath, completes the go-plugin handshake, and returns
// both the client (for Kill) and the typed icplugin.Client (for RPC calls).
// The caller must invoke cli.Kill() when finished.
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
