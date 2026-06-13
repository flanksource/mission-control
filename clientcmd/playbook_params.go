package clientcmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/google/uuid"
	"github.com/samber/lo"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

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
	return buildRemoteRunParamsWithOptions(playbookID, args, ParamFile, playbookConfigID, playbookComponentID, playbookCheckID)
}

func buildRemoteRunParamsWithOptions(playbookID uuid.UUID, args []string, paramFile, configID, componentID, checkID string) (sdk.PlaybookRunParams, error) {
	params, err := readParamFile(paramFile)
	if err != nil {
		return sdk.PlaybookRunParams{}, err
	}
	req := sdk.PlaybookRunParams{
		ID:     playbookID,
		Params: params,
	}

	if err := applyTargetID(&req.ConfigID, configID, "config_id"); err != nil {
		return sdk.PlaybookRunParams{}, err
	}
	if err := applyTargetID(&req.ComponentID, componentID, "component_id"); err != nil {
		return sdk.PlaybookRunParams{}, err
	}
	if err := applyTargetID(&req.CheckID, checkID, "check_id"); err != nil {
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
	return waitForRemotePlaybookRunWithInterval(stderr, client, runID, playbookPollInterval)
}

func waitForRemotePlaybookRunWithInterval(stderr io.Writer, client *sdk.Client, runID string, pollInterval time.Duration) (*sdk.PlaybookSummary, error) {
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
		time.Sleep(pollInterval)
	}
}

// SaveOutputToWriter writes object to file (when set) or w, in yaml or json.
func SaveOutputToWriter(w io.Writer, object any, file string, format string) error {
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
	}
	_, err := fmt.Fprintln(w, out)
	return err
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
