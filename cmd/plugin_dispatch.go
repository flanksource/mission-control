package cmd

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/plugin/manifestcache"
	"github.com/flanksource/incident-commander/plugin/registry"
	"github.com/flanksource/incident-commander/sdk"
)

// pluginMode is the dispatch mode resolved from MCContext at the start of
// each plugin command. Mode selection is not memoized: the user can switch
// contexts between commands, and each subcommand re-resolves to keep the
// behavior obvious.
type pluginMode int

const (
	modeNone  pluginMode = iota
	modeAPI              // Server + Token: forward HTTP to /api/plugins
	modeLocal            // DB / no-API: spawn the plugin binary
)

// resolveMode picks API mode if the current context has Server+Token, else
// falls back to local-binary mode. Returns the resolved MCContext (may be
// nil for local mode).
func resolveMode() (pluginMode, *MCContext, error) {
	if mc, ok := contextHasAPI(); ok {
		return modeAPI, mc, nil
	}
	if registry.PluginPath() == "" {
		return modeNone, nil, errors.New("no API context and no MISSION_CONTROL_PLUGIN_PATH; configure one with `mission-control auth login` or set MISSION_CONTROL_PLUGIN_PATH")
	}
	return modeLocal, nil, nil
}

// refreshAllFromServer fetches every plugin schema the server exposes and
// writes a sidecar entry per plugin. Returns the names written.
func refreshAllFromServer(cmd *cobra.Command, mc *MCContext) ([]string, error) {
	ctx, cancel := gocontext.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	_, flush := startHAR()
	defer func() {
		if err := flush(); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
		}
	}()
	return manifestcache.PopulateAPI(ctx, manifestcache.PopulateOptions{
		Server: mc.Server,
		Token:  mc.Token,
	})
}

// refreshOneFromBinary spawns the named plugin once, captures its manifest,
// and writes the sidecar.
func refreshOneFromBinary(cmd *cobra.Command, name string) (*manifestcache.Entry, error) {
	ctx, cancel := gocontext.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	return manifestcache.PopulateLocal(ctx, name, manifestcache.PopulateOptions{
		BinaryDir: registry.PluginPath(),
	})
}

// dispatchOperation routes an operation invocation to either the HTTP API
// (when an API context is configured) or a locally-spawned plugin binary.
func dispatchOperation(cmd *cobra.Command, plugin, op string, params map[string]string, configID string, raw bool) error {
	mode, mc, err := resolveMode()
	if err != nil {
		return err
	}
	switch mode {
	case modeAPI:
		return dispatchAPI(cmd, mc, plugin, op, params, configID, raw)
	case modeLocal:
		return dispatchLocal(cmd, plugin, op, params, configID, raw)
	}
	return fmt.Errorf("unable to determine dispatch mode")
}

// dispatchAPI forwards the operation to the configured Mission Control
// server. The response is whatever the plugin returned; we honour
// `--json` by passing it through and otherwise pretty-print JSON bodies.
func dispatchAPI(cmd *cobra.Command, mc *MCContext, plugin, op string, params map[string]string, configID string, raw bool) error {
	if params == nil {
		params = map[string]string{}
	}
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode params: %w", err)
	}

	_, flush := startHAR()
	defer func() {
		if err := flush(); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
		}
	}()
	client := newAPIClient(mc, sdk.WithAccept("application/clicky+json,application/json"))

	ctx, cancel := gocontext.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()

	bodyBytes, _, err := client.DispatchPluginOperation(ctx, plugin, op, body, configID)
	if err != nil {
		var serverErr *sdk.ServerError
		if errors.As(err, &serverErr) {
			return fmt.Errorf("forward to %s: %s", mc.Server, formatPluginServerError(serverErr))
		}
		return fmt.Errorf("forward to %s: %w", mc.Server, err)
	}
	return renderResult(cmd, bodyBytes, raw)
}

func formatPluginServerError(err *sdk.ServerError) string {
	if err == nil {
		return ""
	}
	var b strings.Builder
	hasDetails := false
	fmt.Fprintf(&b, "server %d", err.StatusCode)
	if err.Code != "" {
		fmt.Fprintf(&b, "\nCode: %s", err.Code)
		hasDetails = true
	}
	if err.Message != "" {
		fmt.Fprintf(&b, "\nError: %s", err.Message)
		hasDetails = true
	}
	if err.Trace != "" {
		fmt.Fprintf(&b, "\nTrace: %s", err.Trace)
		hasDetails = true
	}
	if err.Time != "" {
		fmt.Fprintf(&b, "\nTime: %s", err.Time)
		hasDetails = true
	}
	if err.Hint != "" {
		fmt.Fprintf(&b, "\nHint: %s", err.Hint)
		hasDetails = true
	}
	if err.Public != "" {
		fmt.Fprintf(&b, "\nPublic: %s", err.Public)
		hasDetails = true
	}
	if len(err.Context) > 0 {
		b.WriteString("\nContext:")
		hasDetails = true
		keys := make([]string, 0, len(err.Context))
		for k := range err.Context {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "\n  %s: %v", k, err.Context[k])
		}
	}
	if err.Stacktrace != "" {
		b.WriteString("\nStacktrace:")
		hasDetails = true
		for _, line := range strings.Split(err.Stacktrace, "\n") {
			fmt.Fprintf(&b, "\n  %s", line)
		}
	}
	if !hasDetails {
		if body := strings.TrimSpace(string(err.Body)); body != "" {
			fmt.Fprintf(&b, ": %s", body)
		}
	}
	return b.String()
}

// renderResult pretty-prints a clicky+json / application/json response,
// or writes raw bytes when --json is set or the body isn't JSON.
func renderResult(cmd *cobra.Command, body []byte, raw bool) error {
	out := cmd.OutOrStdout()
	if raw {
		_, err := out.Write(body)
		return err
	}
	var pretty any
	if err := json.Unmarshal(body, &pretty); err == nil {
		buf, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Fprintln(out, string(buf))
		return nil
	}
	_, err := out.Write(body)
	return err
}
