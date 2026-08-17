package clientcmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/incident-commander/clientapi"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/spf13/cobra"
)

var (
	playbookGetJSON       bool
	playbookHistoryLimit  int
	playbookHistoryStatus []string
)

var GetPlaybook = &cobra.Command{
	Use:          "get <playbook-id|namespace/name|name>",
	Short:        "Get a playbook manifest from the configured Mission Control API",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		item, err := getRemotePlaybook(cmd, args[0])
		if err != nil {
			return err
		}
		manifest, err := playbookManifestFromItem(*item)
		if err != nil {
			return err
		}
		format := "yaml"
		if playbookGetJSON {
			format = "json"
		}
		return SaveOutputToWriter(cmd.OutOrStdout(), manifest, "", format)
	},
}

var DeletePlaybook = &cobra.Command{
	Use:          "delete <playbook-id|namespace/name|name>",
	Short:        "Delete an API-created playbook",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		item, err := getRemotePlaybook(cmd, args[0])
		if err != nil {
			return err
		}
		if item.Source != clientapi.SourceUI {
			return fmt.Errorf("playbook %s/%s was not created through the API and cannot be deleted", item.Namespace, item.Name)
		}
		_, client, err := playbookAPIClient(cmd)
		if err != nil {
			return err
		}
		if _, err := client.DeletePlaybook(cmd.Context(), item.ID); err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "playbook %s/%s deleted\n", item.Namespace, item.Name)
		return err
	},
}

var PlaybookHistory = &cobra.Command{
	Use:          "history <playbook-id|namespace/name|name>",
	Short:        "List execution history for a playbook",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		item, err := getRemotePlaybook(cmd, args[0])
		if err != nil {
			return err
		}
		statuses, err := parsePlaybookRunStatuses(playbookHistoryStatus)
		if err != nil {
			return err
		}
		_, client, err := playbookAPIClient(cmd)
		if err != nil {
			return err
		}
		runs, err := client.ListPlaybookRuns(cmd.Context(), sdk.PlaybookRunListOptions{
			PlaybookID: &item.ID,
			Statuses:   statuses,
			Limit:      playbookHistoryLimit,
		})
		if err != nil {
			return err
		}
		return printClicky(cmd.OutOrStdout(), playbookRuns(runs), "pretty")
	},
}

func getRemotePlaybook(cmd *cobra.Command, ref string) (*clientapi.PlaybookListItem, error) {
	items, err := listRemotePlaybooks(cmd, sdk.PlaybookListOptions{})
	if err != nil {
		return nil, err
	}
	return resolvePlaybookRef(items, ref, playbookNamespace)
}

func playbookManifestFromItem(item clientapi.PlaybookListItem) (*playbookManifest, error) {
	var spec map[string]any
	if err := json.Unmarshal(item.Spec, &spec); err != nil {
		return nil, fmt.Errorf("invalid stored playbook spec: %w", err)
	}
	if item.Title != "" {
		spec["title"] = item.Title
	}
	if item.Description != "" {
		spec["description"] = item.Description
	}
	if item.Icon != "" {
		spec["icon"] = item.Icon
	}
	if item.Category != "" {
		spec["category"] = item.Category
	}
	specJSON, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("marshal playbook spec: %w", err)
	}

	namespace := item.Namespace
	if namespace == "" {
		namespace = "default"
	}
	return &playbookManifest{
		APIVersion: "mission-control.flanksource.com/v1",
		Kind:       "Playbook",
		Metadata: playbookMetadata{
			Name:      item.Name,
			Namespace: namespace,
		},
		Spec: specJSON,
	}, nil
}

func parsePlaybookRunStatuses(values []string) ([]clientapi.PlaybookRunStatus, error) {
	valid := map[clientapi.PlaybookRunStatus]struct{}{
		clientapi.PlaybookRunStatusCancelled:       {},
		clientapi.PlaybookRunStatusTimedOut:        {},
		clientapi.PlaybookRunStatusCompleted:       {},
		clientapi.PlaybookRunStatusFailed:          {},
		clientapi.PlaybookRunStatusPendingApproval: {},
		clientapi.PlaybookRunStatusRunning:         {},
		clientapi.PlaybookRunStatusScheduled:       {},
		clientapi.PlaybookRunStatusSleeping:        {},
		clientapi.PlaybookRunStatusRetrying:        {},
		clientapi.PlaybookRunStatusWaiting:         {},
	}

	var statuses []clientapi.PlaybookRunStatus
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			status := clientapi.PlaybookRunStatus(strings.TrimSpace(part))
			if _, ok := valid[status]; !ok {
				return nil, fmt.Errorf("invalid playbook run status %q", part)
			}
			statuses = append(statuses, status)
		}
	}
	return statuses, nil
}

func init() {
	GetPlaybook.Flags().BoolVar(&playbookGetJSON, "json", false, "Print the playbook manifest as JSON")
	PlaybookHistory.Flags().IntVar(&playbookHistoryLimit, "limit", 20, "Maximum number of runs")
	PlaybookHistory.Flags().StringSliceVar(&playbookHistoryStatus, "status", nil, "Filter by run status (repeatable or comma-separated)")
	clicky.BindAllFlags(PlaybookHistory.Flags(), "format")
	Playbook.AddCommand(GetPlaybook, DeletePlaybook, PlaybookHistory)
}
