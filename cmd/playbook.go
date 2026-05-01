package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/shutdown"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/flanksource/incident-commander/playbook/runner"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

var Playbook = &cobra.Command{
	Use: "playbook",
}

var playbookNamespace string
var outfile string
var outFormat string
var paramFile string
var debugPort int
var playbookWait bool
var playbookPollInterval time.Duration
var playbookConfigID string
var playbookComponentID string
var playbookCheckID string
var playbookListConfigID string
var playbookListComponentID string
var playbookListCheckID string
var playbookListJSON bool
var playbookListOutFormat string

func GetOrCreateAgent(ctx context.Context, name string) (*models.Agent, error) {
	var t models.Agent
	if err := ctx.DB().Where("name = ?", name).First(&t).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if t.ID != uuid.Nil {
		return &t, nil
	}

	t = models.Agent{Name: name}
	tx := ctx.DB().Create(&t)
	return &t, tx.Error
}

func parsePlaybookArgs(ctx context.Context, args []string) (*models.Playbook, *playbook.RunParams, error) {
	p, err := playbook.CreateOrSaveFromFile(ctx, args[0])
	if err != nil {
		return nil, nil, err
	}

	hostname, _ := os.Hostname()
	agent, err := GetOrCreateAgent(ctx, hostname)
	if err != nil {
		return nil, nil, err
	}

	var params = playbook.RunParams{
		Params:  make(map[string]string),
		AgentID: &agent.ID,
	}

	if f, err := os.Open(paramFile); err == nil {
		if err := yamlutil.NewYAMLOrJSONDecoder(f, 1024).Decode(&params.Params); err != nil {
			return nil, nil, err
		}
	}

	for _, arg := range args[1:] {
		parts := strings.Split(arg, "=")
		if len(parts) != 2 {
			logger.Warnf("Invalid param: %s", arg)
			continue
		}

		switch parts[0] {
		case "config", "config_id":
			params.ConfigID = lo.ToPtr(uuid.MustParse(parts[1]))
		case "component", "component_id":
			params.ComponentID = lo.ToPtr(uuid.MustParse(parts[1]))
		case "check", "check_id":
			params.CheckID = lo.ToPtr(uuid.MustParse(parts[1]))
		default:
			params.Params[parts[0]] = parts[1]
		}
	}
	return p, &params, nil
}

func runLocalPlaybook(cmd *cobra.Command, args []string) {
	logger.UseSlog()
	if err := properties.LoadFile("mission-control.properties"); err != nil {
		logger.Errorf(err.Error())
	}
	ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
	if err != nil {
		logger.Fatalf(err.Error())
		return
	}

	shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)

	if debugPort >= 0 {
		e := echo.New(ctx)

		shutdown.AddHookWithPriority("echo", shutdown.PriorityIngress, func() {
			echo.Shutdown(e)
		})

		if debugPort == 0 {
			debugPort = duty.FreePort()
		}
		go echo.Start(e, debugPort)
	}
	shutdown.WaitForSignal()

	p, params, err := parsePlaybookArgs(ctx, args)
	if err != nil {
		shutdown.ShutdownAndExit(1, err.Error())
	}

	sysUser, err := db.GetSystemUser(ctx)
	if err != nil {
		shutdown.ShutdownAndExit(1, err.Error())
	}
	ctx = ctx.WithUser(sysUser)
	run, err := playbook.Run(ctx, p, *params)
	if err != nil {
		logger.Errorf("%+v", err)
		shutdown.ShutdownAndExit(1, err.Error())
		return
	}

	ctx = ctx.WithObject(p, run)

	ctx = ctx.WithNamespace(lo.CoalesceOrEmpty(p.Namespace, api.Namespace))

	var action *v1.PlaybookAction
	var step *models.PlaybookRunAction

	action, step, err = runner.GetNextActionToRun(ctx, *run)
	if err != nil {
		logger.Fatalf(err.Error())
		shutdown.ShutdownAndExit(1, err.Error())
		return
	}

	if action.Retry != nil {
		if delay, err := action.Retry.NextRetryWait(1); err != nil {
			logger.Errorf("error updating run delay: %v", err)
			shutdown.ShutdownAndExit(1, err.Error())
			return
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), delay)
		}
	}

	if action == nil {
		logger.Errorf("No actions to run")
		shutdown.ShutdownAndExit(1, err.Error())
		return
	}

	for action != nil {
		if delay, err := runner.GetDelay(ctx, *p, *run, action, step); err != nil {
			ctx.Errorf("error getting delay %s: %v", action.Name, err)
			break
		} else if delay > 0 {
			if err := run.Delay(ctx.DB(), delay); err != nil {
				ctx.Errorf("error updating run delay: %v", err)
			}

			break
		}

		runAction, err := run.StartAction(ctx.DB(), action.Name)
		if err != nil {
			ctx.Errorf("Error starting action %s: %v", action.Name, err)
			break
		}

		if err = runner.RunAction(ctx, run, runAction); err != nil {
			ctx.Errorf("Error running action %s: %v", action.Name, err)
			break
		}

		action, _, err = runner.GetNextActionToRun(ctx, *run)
		if action != nil && action.Name == runAction.Name {
			ctx.Errorf("%v", ctx.Oops().Errorf("Action cycle detected for: %s", action.Name))
			shutdown.ShutdownAndExit(1, "")
		}

		if err != nil {
			ctx.Errorf("Error getting next action %s: %v", action.Name, err)
			break
		}
	}

	summary, err := playbook.GetPlaybookStatus(ctx, run.ID)

	if err != nil {
		shutdown.ShutdownAndExit(1, err.Error())
	}

	if err := saveOutputToWriter(cmd.OutOrStdout(), summary, outfile, outFormat); err != nil {
		shutdown.ShutdownAndExit(1, err.Error())
	}

	if summary.Run.Status != models.PlaybookRunStatusCompleted {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("Playbook run status: %s ", summary.Run.Status))
	}
}

var Run = &cobra.Command{
	Use:              "run <playbook.yaml|playbook-id|namespace/name|name> [key=value ...]",
	Short:            "Run a playbook from a local YAML file or the configured Mission Control API",
	Args:             cobra.MinimumNArgs(1),
	SilenceUsage:     true,
	PersistentPreRun: PreRun,
	RunE: func(cmd *cobra.Command, args []string) error {
		if isExistingFile(args[0]) {
			runLocalPlaybook(cmd, args)
			return nil
		}
		if debugPort >= 0 {
			return fmt.Errorf("--debug-port is only supported for local YAML playbook runs")
		}
		return runRemotePlaybook(cmd, args)
	},
}

var ListPlaybooks = &cobra.Command{
	Use:              "list",
	Short:            "List playbooks from the configured Mission Control API",
	Args:             cobra.NoArgs,
	SilenceUsage:     true,
	PersistentPreRun: PreRun,
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
		return savePlaybookList(cmd.OutOrStdout(), items, outfile, playbookListJSON || playbookListOutFormat == "json")
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
		return nil, fmt.Errorf("no Mission Control context configured; run `incident-commander context add --server <url> --use`")
	}
	if mcCtx.Server == "" {
		return nil, fmt.Errorf("current context %q must define a server for API playbook commands", mcCtx.Name)
	}
	if mcCtx.Token == "" {
		if err := ensureContextToken(cmd, mcCtx, cmd.ErrOrStderr()); err != nil {
			return nil, err
		}
		cfg.SetContext(*mcCtx)
		if err := SaveConfig(cfg); err != nil {
			return nil, err
		}
	}
	return mcCtx, nil
}

func retryAfterAPIBaseUpgrade(mcCtx *MCContext, err error) (bool, error) {
	if !errors.Is(err, sdk.ErrHTMLResponse) {
		return false, err
	}
	upgraded, upErr := ensureAPIBase(mcCtx)
	if upErr != nil {
		return false, fmt.Errorf("%w (probe failed: %v)", err, upErr)
	}
	if !upgraded {
		return false, err
	}
	return true, nil
}

func playbookAPIClient(cmd *cobra.Command) (*MCContext, *sdk.Client, error) {
	mcCtx, err := currentAPIContext(cmd)
	if err != nil {
		return nil, nil, err
	}
	return mcCtx, sdk.New(mcCtx.Server, mcCtx.Token), nil
}

func listRemotePlaybooks(cmd *cobra.Command, opts sdk.PlaybookListOptions) ([]api.PlaybookListItem, error) {
	mcCtx, client, err := playbookAPIClient(cmd)
	if err != nil {
		return nil, err
	}
	items, err := client.ListPlaybooks(opts)
	if upgraded, upgradeErr := retryAfterAPIBaseUpgrade(mcCtx, err); upgradeErr != nil {
		return nil, upgradeErr
	} else if upgraded {
		items, err = sdk.New(mcCtx.Server, mcCtx.Token).ListPlaybooks(opts)
	}
	return items, err
}

func runRemotePlaybook(cmd *cobra.Command, args []string) error {
	if targetCount(playbookConfigID, playbookComponentID, playbookCheckID) > 1 {
		return fmt.Errorf("provide at most one of --config-id, --component-id, or --check-id")
	}

	mcCtx, client, err := playbookAPIClient(cmd)
	if err != nil {
		return err
	}

	playbooks, err := client.ListPlaybooks(sdk.PlaybookListOptions{})
	if upgraded, upgradeErr := retryAfterAPIBaseUpgrade(mcCtx, err); upgradeErr != nil {
		return upgradeErr
	} else if upgraded {
		client = sdk.New(mcCtx.Server, mcCtx.Token)
		playbooks, err = client.ListPlaybooks(sdk.PlaybookListOptions{})
	}
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
	if upgraded, upgradeErr := retryAfterAPIBaseUpgrade(mcCtx, err); upgradeErr != nil {
		return upgradeErr
	} else if upgraded {
		client = sdk.New(mcCtx.Server, mcCtx.Token)
		response, err = client.RunPlaybook(params)
	}
	if err != nil {
		return err
	}

	if !playbookWait {
		return saveOutputToWriter(cmd.OutOrStdout(), response, outfile, outFormat)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "playbook %s/%s run %s scheduled for %s\n", item.Namespace, item.Name, response.RunID, response.StartsAt)
	summary, err := waitForRemotePlaybookRun(cmd.ErrOrStderr(), client, response.RunID)
	if err != nil {
		return err
	}
	if err := saveOutputToWriter(cmd.OutOrStdout(), summary, outfile, outFormat); err != nil {
		return err
	}
	if summary.Run.Status != models.PlaybookRunStatusCompleted {
		return fmt.Errorf("playbook run status: %s", summary.Run.Status)
	}
	return nil
}

func resolvePlaybookRef(playbooks []api.PlaybookListItem, ref string, namespace string) (*api.PlaybookListItem, error) {
	if id, err := uuid.Parse(ref); err == nil {
		for i := range playbooks {
			if playbooks[i].ID == id {
				return &playbooks[i], nil
			}
		}
		return nil, fmt.Errorf("playbook id %s not found", ref)
	}

	if ns, name, ok := strings.Cut(ref, "/"); ok {
		for i := range playbooks {
			if playbooks[i].Namespace == ns && playbooks[i].Name == name {
				return &playbooks[i], nil
			}
		}
		return nil, fmt.Errorf("playbook %s not found", ref)
	}

	var matches []api.PlaybookListItem
	for _, item := range playbooks {
		if item.Name == ref && item.Namespace == namespace {
			matches = append(matches, item)
		}
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}

	if len(matches) == 0 {
		for _, item := range playbooks {
			if item.Name == ref {
				matches = append(matches, item)
			}
		}
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if len(matches) > 1 {
		names := lo.Map(matches, func(item api.PlaybookListItem, _ int) string {
			return item.Namespace + "/" + item.Name
		})
		return nil, fmt.Errorf("playbook name %q is ambiguous; use namespace/name (matches: %s)", ref, strings.Join(names, ", "))
	}
	return nil, fmt.Errorf("playbook %q not found", ref)
}

func buildRemoteRunParams(playbookID uuid.UUID, args []string) (sdk.PlaybookRunParams, error) {
	params, err := readParamFile(paramFile)
	if err != nil {
		return sdk.PlaybookRunParams{}, err
	}
	req := sdk.PlaybookRunParams{
		ID:     playbookID,
		Params: params,
	}

	if err := applyTargetID(&req.ConfigID, playbookConfigID, "config_id"); err != nil {
		return sdk.PlaybookRunParams{}, err
	}
	if err := applyTargetID(&req.ComponentID, playbookComponentID, "component_id"); err != nil {
		return sdk.PlaybookRunParams{}, err
	}
	if err := applyTargetID(&req.CheckID, playbookCheckID, "check_id"); err != nil {
		return sdk.PlaybookRunParams{}, err
	}

	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok || key == "" {
			return sdk.PlaybookRunParams{}, fmt.Errorf("invalid parameter %q, expected key=value", arg)
		}
		switch key {
		case "config", "config_id":
			if err := applyTargetID(&req.ConfigID, value, "config_id"); err != nil {
				return sdk.PlaybookRunParams{}, err
			}
		case "component", "component_id":
			if err := applyTargetID(&req.ComponentID, value, "component_id"); err != nil {
				return sdk.PlaybookRunParams{}, err
			}
		case "check", "check_id":
			if err := applyTargetID(&req.CheckID, value, "check_id"); err != nil {
				return sdk.PlaybookRunParams{}, err
			}
		default:
			if req.Params == nil {
				req.Params = make(map[string]string)
			}
			req.Params[key] = value
		}
	}

	if targetCount(uuidPtrString(req.ConfigID), uuidPtrString(req.ComponentID), uuidPtrString(req.CheckID)) > 1 {
		return sdk.PlaybookRunParams{}, fmt.Errorf("provide at most one of config_id, component_id, or check_id")
	}
	return req, nil
}

func readParamFile(file string) (map[string]string, error) {
	params := make(map[string]string)
	if file == "" {
		return params, nil
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := yamlutil.NewYAMLOrJSONDecoder(f, 1024).Decode(&params); err != nil {
		return nil, err
	}
	return params, nil
}

func applyTargetID(target **uuid.UUID, value string, name string) error {
	if value == "" {
		return nil
	}
	if *target != nil {
		return fmt.Errorf("%s was provided more than once", name)
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", name, err)
	}
	*target = &id
	return nil
}

func uuidPtrString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func targetCount(values ...string) int {
	count := 0
	for _, value := range values {
		if value != "" {
			count++
		}
	}
	return count
}

func waitForRemotePlaybookRun(stderr io.Writer, client *sdk.Client, runID string) (*sdk.PlaybookSummary, error) {
	lastRunStatus := ""
	lastActions := make(map[string]string)
	for {
		summary, err := client.GetPlaybookRunStatus(runID)
		if err != nil {
			return nil, err
		}

		runStatus := string(summary.Run.Status)
		if runStatus != lastRunStatus {
			fmt.Fprintf(stderr, "run %s status=%s\n", runID, runStatus)
			lastRunStatus = runStatus
		}
		for _, action := range summary.Actions {
			key := action.ID.String()
			status := string(action.Status)
			if lastActions[key] == status {
				continue
			}
			if action.Error != nil && *action.Error != "" {
				fmt.Fprintf(stderr, "action %s status=%s error=%s\n", action.Name, status, *action.Error)
			} else {
				fmt.Fprintf(stderr, "action %s status=%s\n", action.Name, status)
			}
			lastActions[key] = status
		}

		if lo.Contains(models.PlaybookRunStatusFinalStates, summary.Run.Status) {
			return summary, nil
		}
		time.Sleep(playbookPollInterval)
	}
}

func saveOutput(object any, file string, format string) {
	_ = saveOutputToWriter(os.Stdout, object, file, format)
}

func saveOutputToWriter(w io.Writer, object any, file string, format string) error {
	var out string
	if format == "yaml" {
		b, _ := yaml.Marshal(object)
		out = string(b)
	} else {
		b, _ := json.MarshalIndent(object, "", "  ")
		out = string(b)
	}

	if file != "" {
		return os.WriteFile(file, []byte(out), 0600)
	} else {
		_, err := fmt.Fprintln(w, out)
		return err
	}
}

func savePlaybookList(w io.Writer, items []api.PlaybookListItem, file string, asJSON bool) error {
	var out bytes.Buffer
	if asJSON {
		b, _ := json.MarshalIndent(items, "", "  ")
		out.Write(b)
		out.WriteByte('\n')
	} else {
		tw := tabwriter.NewWriter(&out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "CATEGORY\tNAMESPACE\tNAME\tUUID")
		for _, item := range items {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", item.Category, item.Namespace, item.Name, item.ID)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	if file != "" {
		return os.WriteFile(file, out.Bytes(), 0600)
	}
	_, err := w.Write(out.Bytes())
	return err
}

var Submit = &cobra.Command{
	Use:              "submit playbook playbook.yaml params.yaml",
	Args:             cobra.MinimumNArgs(1),
	PersistentPreRun: PreRun,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger.UseSlog()
		if err := properties.LoadFile("mission-control.properties"); err != nil {
			logger.Errorf(err.Error())
		}
		ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
		if err != nil {
			return err
		}

		shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)

		shutdown.WaitForSignal()

		p, params, err := parsePlaybookArgs(ctx, args)
		params.AgentID = nil
		if err != nil {
			shutdown.ShutdownAndExit(1, err.Error())
		}

		sysUser, err := db.GetSystemUser(ctx)
		if err != nil {
			logger.Fatalf(err.Error())
			os.Exit(1)
		}
		ctx = ctx.WithUser(sysUser)

		run, err := playbook.Run(ctx, p, *params)
		if err != nil {
			return err
		}

		fmt.Println(logger.Pretty(run))

		return nil
	},
}

func init() {
	Playbook.PersistentFlags().StringVarP(&playbookNamespace, "namespace", "n", "default", "Namespace for playbook to run under")
	Playbook.PersistentFlags().StringVarP(&paramFile, "params", "p", "", "YAML/JSON file containing parameters")
	Run.Flags().IntVar(&debugPort, "debug-port", -1, "Start an HTTP server to use the /debug routes, Use -1 to disable and 0 to pick a free port")
	Run.Flags().BoolVar(&playbookWait, "wait", true, "Wait for the playbook run to finish")
	Run.Flags().DurationVar(&playbookPollInterval, "poll-interval", 2*time.Second, "Polling interval used with --wait")
	Run.Flags().StringVar(&playbookConfigID, "config-id", "", "Config ID to run the playbook against")
	Run.Flags().StringVar(&playbookComponentID, "component-id", "", "Component ID to run the playbook against")
	Run.Flags().StringVar(&playbookCheckID, "check-id", "", "Check ID to run the playbook against")
	Run.Flags().StringVarP(&outfile, "out-file", "o", "", "Write playbook summary to file instead of stdout")
	Run.Flags().StringVarP(&outFormat, "out-format", "f", "yaml", "Format of output file or stdout (yaml or json)")

	ListPlaybooks.Flags().StringVar(&playbookListConfigID, "config-id", "", "Only list playbooks runnable for this config ID")
	ListPlaybooks.Flags().StringVar(&playbookListComponentID, "component-id", "", "Only list playbooks runnable for this component ID")
	ListPlaybooks.Flags().StringVar(&playbookListCheckID, "check-id", "", "Only list playbooks runnable for this check ID")
	ListPlaybooks.Flags().StringVarP(&outfile, "out-file", "o", "", "Write playbook list to file instead of stdout")
	ListPlaybooks.Flags().BoolVar(&playbookListJSON, "json", false, "Print the full playbook list as JSON")
	ListPlaybooks.Flags().StringVarP(&playbookListOutFormat, "out-format", "f", "table", "Format of output file or stdout (table or json)")

	Playbook.AddCommand(ListPlaybooks, Run, Submit)
	Root.AddCommand(Playbook)
}
