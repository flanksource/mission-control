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

	"github.com/flanksource/incident-commander/clientcmd"
	"github.com/flanksource/incident-commander/plugin/api"
	"github.com/flanksource/incident-commander/plugin/machinery/local"
	"github.com/flanksource/incident-commander/plugin/manifestcache"
)

func init() {
	clientcmd.LocalPluginDispatch = dispatchLocal
	clientcmd.LocalPluginRefresh = localPluginRefresh
}

// localPluginRefresh refreshes the plugin command cache from a locally
// installed plugin binary. Wired into clientcmd.LocalPluginRefresh.
func localPluginRefresh(cmd *cobra.Command, args []string) ([]string, error) {
	name := args[0]
	entry, err := refreshPluginCacheFromBinary(cmd, name)
	if err != nil {
		return nil, err
	}
	if entry != nil && entry.Service.Name != "" {
		return []string{entry.Service.Name}, nil
	}
	return []string{name}, nil
}

func refreshPluginCacheFromBinary(cmd *cobra.Command, name string) (*manifestcache.Entry, error) {
	cacheDir, err := clientcmd.CurrentContextPluginCacheDir()
	if err != nil {
		return nil, err
	}

	ctx, cancel := gocontext.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	return manifestcache.PopulateLocal(ctx, name, manifestcache.PopulateOptions{
		BinaryDir: local.PluginPath(),
		CacheDir:  cacheDir,
	})
}

// dispatchLocal spawns the plugin binary, completes the gRPC handshake,
// invokes the operation, and shuts the plugin down. Used when the CLI has
// no API context configured (DB-only / local dev).
func dispatchLocal(cmd *cobra.Command, pluginName, op string, params map[string]string, configID string, raw bool) error {
	binPath, err := manifestcache.FindBinaryFor(pluginName)
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
	if _, err := pluginCli.Service.RegisterPlugin(registerCtx, &api.RegisterRequest{
		HostProtocolVersion: uint32(api.ProtocolVersion),
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
	resp, err := pluginCli.Service.Invoke(invokeCtx, &api.InvokeRequest{
		Operation:    op,
		ParamsJson:   body,
		ConfigItemId: configID,
	})
	if err != nil {
		return fmt.Errorf("invoke %s/%s: %w", pluginName, op, err)
	}
	if resp.ErrorMessage != "" {
		return fmt.Errorf("plugin error: %s (%s)", resp.ErrorMessage, resp.ErrorCode)
	}

	return clientcmd.RenderResult(cmd, resp.Result, raw)
}

// dialPlugin spawns binPath, completes the go-plugin handshake, and returns
// both the client (for Kill) and the typed local.Client (for RPC calls).
// The caller must invoke cli.Kill() when finished.
func dialPlugin(binPath string) (*goplugin.Client, *local.Client, error) {
	cmd := osExec.Command(binPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("%s=%s", api.Handshake.MagicCookieKey, api.Handshake.MagicCookieValue),
	)
	cli := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: api.Handshake,
		Plugins: map[string]goplugin.Plugin{
			api.PluginName: &local.GRPCPlugin{},
		},
		Cmd:              cmd,
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Managed:          true,
	})
	rpcClient, err := cli.Client()
	if err != nil {
		cli.Kill()
		return nil, nil, fmt.Errorf("plugin rpc: %w", err)
	}
	raw, err := rpcClient.Dispense(api.PluginName)
	if err != nil {
		cli.Kill()
		return nil, nil, fmt.Errorf("plugin dispense: %w", err)
	}
	pluginCli, ok := raw.(*local.Client)
	if !ok {
		cli.Kill()
		return nil, nil, fmt.Errorf("plugin: unexpected dispense type %T", raw)
	}
	return cli, pluginCli, nil
}
