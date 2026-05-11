package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/flanksource/incident-commander/sdk"
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
	Root.AddCommand(PluginCmd)
}

func runPluginOp(cmd *cobra.Command, args []string) error {
	server, authHeader, err := pluginServerAndAuth()
	if err != nil {
		return err
	}

	params := pluginOpts.Params.values
	if params == nil {
		params = map[string]string{}
	}
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}

	client := sdk.NewWithAuthHeader(server, authHeader)
	respBody, err := client.InvokePluginOperation(args[0], args[1], pluginOpts.ConfigID, body)
	if err != nil {
		return err
	}

	if pluginOpts.RawJSON {
		_, err = cmd.OutOrStdout().Write(respBody)
		return err
	}

	var pretty any
	if err := json.Unmarshal(respBody, &pretty); err == nil {
		out, err := json.MarshalIndent(pretty, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	_, err = cmd.OutOrStdout().Write(respBody)
	return err
}

func pluginServerAndAuth() (string, string, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return "", "", err
	}

	mcCtx := cfg.CurrentMCContext()
	if mcCtx == nil || mcCtx.Server == "" {
		return "", "", fmt.Errorf("no Mission Control server configured; select a context with server")
	}
	server := mcCtx.Server

	if _, err := url.ParseRequestURI(server); err != nil {
		return "", "", fmt.Errorf("invalid Mission Control server URL %q: %w", server, err)
	}

	if mcCtx.Token != "" {
		return server, "Bearer " + mcCtx.Token, nil
	}

	basicAuth := strings.TrimSpace(os.Getenv("PLUGIN_SERVER_AUTH"))
	if strings.HasPrefix(strings.ToLower(basicAuth), "basic ") {
		basicAuth = strings.TrimSpace(basicAuth[len("basic "):])
	}
	if basicAuth == "" {
		return server, "", nil
	}

	decoded, err := base64.StdEncoding.DecodeString(basicAuth)
	if err != nil {
		return "", "", fmt.Errorf("PLUGIN_SERVER_AUTH must be base64(username:password): %w", err)
	}
	if !strings.Contains(string(decoded), ":") {
		return "", "", fmt.Errorf("PLUGIN_SERVER_AUTH must decode to username:password")
	}

	return server, "Basic " + basicAuth, nil
}
