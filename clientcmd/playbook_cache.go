package clientcmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/spf13/cobra"
)

const playbookCacheFile = "playbooks.json"

func contextPlaybookCacheDir(mc *MCContext) string {
	return filepath.Join(contextCacheDir(mc), "playbooks")
}

func populatePlaybookCache(cmd *cobra.Command, dir string) ([]string, error) {
	items, err := listRemotePlaybooks(cmd, sdk.PlaybookListOptions{})
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, playbookCacheFile), data, 0o600); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, playbookRef(item))
	}
	return names, nil
}

func listCachedPlaybooksFromDir(dir string) ([]api.PlaybookListItem, error) {
	data, err := os.ReadFile(filepath.Join(dir, playbookCacheFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var items []api.PlaybookListItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func RegisterContextCachedPlaybookCommands(root *cobra.Command) error {
	mc, err := currentMCContext()
	if err != nil {
		return err
	}
	if mc == nil {
		return nil
	}
	items, err := listCachedPlaybooksFromDir(contextPlaybookCacheDir(mc))
	if err != nil {
		return err
	}
	return registerCachedPlaybookCommands(Playbook, root, items)
}

func registerCachedPlaybookCommands(playbookRoot, root *cobra.Command, items []api.PlaybookListItem) error {
	if playbookRoot == nil {
		return nil
	}
	runRoot := findChildCommand(playbookRoot, "run")
	if runRoot == nil {
		return nil
	}
	sort.SliceStable(items, func(i, j int) bool { return playbookRef(items[i]) < playbookRef(items[j]) })
	for _, item := range items {
		name := cachedPlaybookCommandName(runRoot, item)
		if name == "" {
			continue
		}
		if !commandExists(runRoot, name) {
			runRoot.AddCommand(newCachedPlaybookCommand(item, name))
		}
		if root != nil && !commandExists(root, name) {
			root.AddCommand(newCachedPlaybookCommand(item, name))
		}
	}
	return nil
}

func findChildCommand(parent *cobra.Command, name string) *cobra.Command {
	for _, child := range parent.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}

func cachedPlaybookCommandName(runRoot *cobra.Command, item api.PlaybookListItem) string {
	name := strings.TrimSpace(item.Name)
	if name == "" {
		return ""
	}
	if !commandExists(runRoot, name) {
		return name
	}
	if item.Namespace != "" {
		return safeContextName(item.Namespace + "-" + item.Name)
	}
	return safeContextName(item.ID.String())
}

func newCachedPlaybookCommand(item api.PlaybookListItem, name string) *cobra.Command {
	params := playbookParametersFromItem(item)
	values := map[string]*string{}
	var wait = true
	var pollInterval = 2 * time.Second
	var configID, componentID, checkID, paramFile, outFile, outFormat string
	short := item.Description
	if short == "" {
		short = item.Title
	}
	if short == "" {
		short = fmt.Sprintf("Run playbook %s", playbookRef(item))
	}
	cmd := &cobra.Command{
		Use:          name,
		Short:        short,
		Long:         formatCachedPlaybookLong(item, params),
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if targetCount(configID, componentID, checkID) > 1 {
				return fmt.Errorf("provide at most one of --config-id, --component-id, or --check-id")
			}
			args := make([]string, 0, len(values))
			for _, p := range params {
				value := ""
				if values[p.Name] != nil {
					value = *values[p.Name]
				}
				if p.Required && strings.TrimSpace(value) == "" {
					return fmt.Errorf("missing required parameter: --%s", p.Name)
				}
				if strings.TrimSpace(value) != "" {
					args = append(args, p.Name+"="+value)
				}
			}
			req, err := buildRemoteRunParamsWithOptions(item.ID, args, paramFile, configID, componentID, checkID)
			if err != nil {
				return err
			}
			_, client, err := playbookAPIClient(cmd)
			if err != nil {
				return err
			}
			response, err := client.RunPlaybook(req)
			if err != nil {
				return err
			}
			if !wait {
				return SaveOutputToWriter(cmd.OutOrStdout(), response, outFile, outFormat)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "playbook %s run %s scheduled for %s\n", playbookRef(item), response.RunID, response.StartsAt)
			summary, err := waitForRemotePlaybookRunWithInterval(cmd.ErrOrStderr(), client, response.RunID, pollInterval)
			if err != nil {
				return err
			}
			if err := SaveOutputToWriter(cmd.OutOrStdout(), summary, outFile, outFormat); err != nil {
				return err
			}
			if summary.Run.Status != "completed" {
				return fmt.Errorf("playbook run status: %s", summary.Run.Status)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&wait, "wait", true, "Wait for the playbook run to finish")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 2*time.Second, "Polling interval used with --wait")
	cmd.Flags().StringVar(&configID, "config-id", "", "Config ID to run the playbook against")
	cmd.Flags().StringVar(&componentID, "component-id", "", "Component ID to run the playbook against")
	cmd.Flags().StringVar(&checkID, "check-id", "", "Check ID to run the playbook against")
	cmd.Flags().StringVarP(&paramFile, "params", "p", "", "YAML/JSON file containing parameters")
	cmd.Flags().StringVarP(&outFile, "out-file", "o", "", "Write playbook summary to file instead of stdout")
	cmd.Flags().StringVarP(&outFormat, "out-format", "f", "yaml", "Format of output file or stdout (yaml or json)")
	for _, p := range params {
		if p.Name == "" || cmd.Flags().Lookup(p.Name) != nil {
			continue
		}
		value := fmt.Sprint(p.Default)
		values[p.Name] = &value
		cmd.Flags().StringVar(values[p.Name], p.Name, value, p.Description)
	}
	return cmd
}

func playbookParametersFromItem(item api.PlaybookListItem) []v1.PlaybookParameter {
	if len(item.Parameters) == 0 {
		return nil
	}
	var params []v1.PlaybookParameter
	_ = json.Unmarshal(item.Parameters, &params)
	return params
}

func formatCachedPlaybookLong(item api.PlaybookListItem, params []v1.PlaybookParameter) string {
	var b strings.Builder
	if item.Description != "" {
		b.WriteString(item.Description)
		b.WriteString("\n\n")
	}
	fmt.Fprintf(&b, "Playbook: %s\n", playbookRef(item))
	if item.ID.String() != "" {
		fmt.Fprintf(&b, "ID: %s\n", item.ID)
	}
	if len(params) > 0 {
		b.WriteString("\nParameters:\n")
		for _, p := range params {
			marker := " "
			if p.Required {
				marker = "*"
			}
			typ := string(p.Type)
			if typ == "" {
				typ = "text"
			}
			fmt.Fprintf(&b, "  %s --%s (%s)", marker, p.Name, typ)
			if p.Default != "" {
				fmt.Fprintf(&b, " [default: %s]", p.Default)
			}
			if p.Description != "" {
				fmt.Fprintf(&b, "\n      %s", p.Description)
			}
			b.WriteByte('\n')
		}
		b.WriteString("\n* = required")
	}
	return strings.TrimRight(b.String(), "\n")
}

func playbookRef(item api.PlaybookListItem) string {
	if item.Namespace == "" {
		return item.Name
	}
	return item.Namespace + "/" + item.Name
}
