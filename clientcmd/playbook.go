package clientcmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/spf13/cobra"
)

// Playbook is the parent command for playbook operations. The client owns the
// remote surfaces (list, run); the server binary attaches local execution via
// LocalRunHandler and the Submit subcommand.
var Playbook = &cobra.Command{
	Use: "playbook",
}

// LocalRunHandler, when set by the full mission-control binary, executes a
// playbook from a local YAML file. The slim faro client leaves it nil and
// only supports running playbooks by id/name against a remote server.
var LocalRunHandler func(cmd *cobra.Command, args []string) error

// Shared parameter flag (read by both the remote client and local execution).
var ParamFile string

var (
	playbookNamespace       string
	playbookWait            bool
	playbookPollInterval    time.Duration
	playbookConfigID        string
	playbookComponentID     string
	playbookCheckID         string
	playbookListConfigID    string
	playbookListComponentID string
	playbookListCheckID     string
	playbookListJSON        bool
)

var Run = &cobra.Command{
	Use:          "run <playbook.yaml|playbook-id|namespace/name|name> [key=value ...]",
	Short:        "Run a playbook from a local YAML file or the configured Mission Control API",
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if isExistingFile(args[0]) {
			if LocalRunHandler == nil {
				return fmt.Errorf("local playbook execution is not supported by this binary; provide a playbook id or namespace/name")
			}
			return LocalRunHandler(cmd, args)
		}
		if f := cmd.Flags().Lookup("debug-port"); f != nil && f.Changed {
			return fmt.Errorf("--debug-port is only supported for local YAML playbook runs")
		}
		return runRemotePlaybook(cmd, args)
	},
}

var ListPlaybooks = &cobra.Command{
	Use:          "list",
	Short:        "List playbooks from the configured Mission Control API",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if targetCount(playbookListConfigID, playbookListComponentID, playbookListCheckID) > 1 {
			return fmt.Errorf("provide at most one of --config-id, --component-id, or --check-id")
		}

		items, err := listRemotePlaybooks(cmd, sdk.PlaybookListOptions{
			ConfigID:    playbookListConfigID,
			CheckID:     playbookListCheckID,
			ComponentID: playbookListComponentID,
		})
		if err != nil {
			return err
		}
		return savePlaybookList(cmd.OutOrStdout(), items, playbookListJSON)
	},
}

func isExistingFile(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir()
}

func currentAPIContext(cmd *cobra.Command) (*MCContext, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	mcCtx := cfg.CurrentMCContext()
	if mcCtx == nil {
		return nil, fmt.Errorf("no Mission Control context configured; run `context add --server <url> --use`")
	}
	if mcCtx.Server == "" {
		return nil, fmt.Errorf("current context %q must define a server for API playbook commands", mcCtx.Name)
	}
	if !mcCtx.HasAuth() {
		if err := EnsureContextToken(cmd, mcCtx, cmd.ErrOrStderr()); err != nil {
			return nil, err
		}
		cfg.SetContext(*mcCtx)
		if err := SaveConfig(cfg); err != nil {
			return nil, err
		}
	}
	return mcCtx, nil
}

func playbookAPIClient(cmd *cobra.Command) (*MCContext, *sdk.Client, error) {
	mcCtx, err := currentAPIContext(cmd)
	if err != nil {
		return nil, nil, err
	}
	return mcCtx, NewAPIClient(mcCtx), nil
}

func listRemotePlaybooks(cmd *cobra.Command, opts sdk.PlaybookListOptions) ([]api.PlaybookListItem, error) {
	_, client, err := playbookAPIClient(cmd)
	if err != nil {
		return nil, err
	}
	return client.ListPlaybooks(opts)
}

func runRemotePlaybook(cmd *cobra.Command, args []string) error {
	if targetCount(playbookConfigID, playbookComponentID, playbookCheckID) > 1 {
		return fmt.Errorf("provide at most one of --config-id, --component-id, or --check-id")
	}

	_, client, err := playbookAPIClient(cmd)
	if err != nil {
		return err
	}

	playbooks, err := client.ListPlaybooks(sdk.PlaybookListOptions{})
	if err != nil {
		return err
	}

	item, err := resolvePlaybookRef(playbooks, args[0], playbookNamespace)
	if err != nil {
		return err
	}

	params, err := buildRemoteRunParams(item.ID, args[1:])
	if err != nil {
		return err
	}

	response, err := client.RunPlaybook(params)
	if err != nil {
		return err
	}

	playbookRef := item.Namespace + "/" + item.Name
	if !playbookWait {
		return Log(cmd.OutOrStdout(), map[string]any{
			"type":      "playbook_run_scheduled",
			"playbook":  playbookRef,
			"run_id":    response.RunID,
			"starts_at": response.StartsAt,
		})
	}

	logger.V(1).Infof("type=playbook_run_scheduled playbook=%s run_id=%s starts_at=%s", playbookRef, response.RunID, response.StartsAt)
	summary, err := waitForRemotePlaybookRun(cmd.ErrOrStderr(), client, response.RunID)
	if err != nil {
		return err
	}
	if err := PrintPlaybookActionResults(cmd.OutOrStdout(), summary); err != nil {
		return err
	}
	if summary.Run.Status != models.PlaybookRunStatusCompleted {
		return fmt.Errorf("playbook run status: %s", summary.Run.Status)
	}
	return nil
}

func init() {
	clicky.BindAllFlags(Playbook.PersistentFlags(), "format")
	Playbook.PersistentFlags().StringVarP(&playbookNamespace, "namespace", "n", "default", "Namespace for playbook to run under")
	Playbook.PersistentFlags().StringVarP(&ParamFile, "params", "p", "", "YAML/JSON file containing parameters")
	Run.Flags().BoolVar(&playbookWait, "wait", true, "Wait for the playbook run to finish")
	Run.Flags().DurationVar(&playbookPollInterval, "poll-interval", 2*time.Second, "Polling interval used with --wait")
	Run.Flags().StringVar(&playbookConfigID, "config-id", "", "Config ID to run the playbook against")
	Run.Flags().StringVar(&playbookComponentID, "component-id", "", "Component ID to run the playbook against")
	Run.Flags().StringVar(&playbookCheckID, "check-id", "", "Check ID to run the playbook against")

	ListPlaybooks.Flags().StringVar(&playbookListConfigID, "config-id", "", "Only list playbooks runnable for this config ID")
	ListPlaybooks.Flags().StringVar(&playbookListComponentID, "component-id", "", "Only list playbooks runnable for this component ID")
	ListPlaybooks.Flags().StringVar(&playbookListCheckID, "check-id", "", "Only list playbooks runnable for this check ID")
	ListPlaybooks.Flags().BoolVar(&playbookListJSON, "json", false, "Print the full playbook list as JSON")

	Playbook.AddCommand(ListPlaybooks, Run)
}

type PlaybookRunOutput struct {
	Result  any                    `json:"result,omitempty" yaml:"result,omitempty"`
	Results []PlaybookActionOutput `json:"results,omitempty" yaml:"results,omitempty"`
}

type PlaybookActionOutput struct {
	Name   string `json:"name" yaml:"name"`
	Result any    `json:"result" yaml:"result"`
}

func PlaybookActionResults(summary *sdk.PlaybookSummary) PlaybookRunOutput {
	if summary == nil || len(summary.Actions) == 0 {
		return PlaybookRunOutput{Result: map[string]any{}}
	}

	if len(summary.Actions) == 1 {
		return PlaybookRunOutput{Result: summary.Actions[0].Result}
	}

	results := make([]PlaybookActionOutput, 0, len(summary.Actions))
	for _, action := range summary.Actions {
		results = append(results, PlaybookActionOutput{
			Name:   action.Name,
			Result: action.Result,
		})
	}
	return PlaybookRunOutput{Results: results}
}

func PrintPlaybookActionResults(w io.Writer, summary *sdk.PlaybookSummary) error {
	return printClicky(w, PlaybookActionResults(summary), "pretty")
}

func printClicky(w io.Writer, data any, defaultFormat string) error {
	opts := clicky.Flags.FormatOptions
	if err := opts.ParseFormatSpec(); err != nil {
		return err
	}

	if len(opts.Sinks) == 0 {
		return writeClickyOutput(w, data, clicky.FormatOptions{Format: defaultFormat}, opts)
	}

	for _, sink := range opts.Sinks {
		sinkOpts := opts
		sinkOpts.Sinks = nil
		sinkOpts.Format = sink.Format
		sinkOpts.JSON, sinkOpts.YAML, sinkOpts.CSV = false, false, false
		sinkOpts.HTML, sinkOpts.Markdown, sinkOpts.Pretty = false, false, false
		sinkOpts.PDF, sinkOpts.Slack = false, false
		if sink.File == "" {
			if err := writeClickyOutput(w, data, sinkOpts); err != nil {
				return err
			}
			continue
		}
		sinkOpts.Output = sink.File
		if err := clicky.Formatter.FormatToFile(sinkOpts, data); err != nil {
			return err
		}
	}
	return nil
}

func writeClickyOutput(w io.Writer, data any, opts ...clicky.FormatOptions) error {
	out, err := clicky.Format(data, opts...)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, out); err != nil {
		return err
	}
	if !strings.HasSuffix(out, "\n") {
		_, err = fmt.Fprintln(w)
	}
	return err
}
