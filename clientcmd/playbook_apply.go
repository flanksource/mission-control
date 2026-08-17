package clientcmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/flanksource/incident-commander/clientapi"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

var playbookApplyFile string

var ApplyPlaybook = &cobra.Command{
	Use:          "apply -f <playbook.yaml>",
	Short:        "Create or update a playbook from a manifest",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		manifest, err := readPlaybookManifest(cmd, playbookApplyFile)
		if err != nil {
			return err
		}
		params, err := parsePlaybookManifest(manifest)
		if err != nil {
			return err
		}

		_, client, err := playbookAPIClient(cmd)
		if err != nil {
			return err
		}
		result, err := client.ApplyPlaybook(cmd.Context(), *params)
		if err != nil {
			return err
		}
		action := "configured"
		if result.Created {
			action = "created"
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "playbook %s/%s %s\n", params.Namespace, params.Name, action)
		return err
	},
}

type playbookManifest struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Metadata   playbookMetadata `json:"metadata"`
	Spec       json.RawMessage  `json:"spec"`
}

type playbookMetadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

func readPlaybookManifest(cmd *cobra.Command, filename string) ([]byte, error) {
	if filename == "-" {
		return io.ReadAll(cmd.InOrStdin())
	}
	return os.ReadFile(filename)
}

func parsePlaybookManifest(manifest []byte) (*sdk.PlaybookApplyParams, error) {
	jsonManifest, err := yaml.YAMLToJSON(manifest)
	if err != nil {
		return nil, fmt.Errorf("invalid playbook YAML: %w", err)
	}

	var playbook playbookManifest
	if err := json.Unmarshal(jsonManifest, &playbook); err != nil {
		return nil, fmt.Errorf("invalid playbook manifest: %w", err)
	}
	if playbook.Kind != "" && !strings.EqualFold(playbook.Kind, "Playbook") {
		return nil, fmt.Errorf("manifest kind must be Playbook, got %q", playbook.Kind)
	}
	if strings.TrimSpace(playbook.Metadata.Name) == "" {
		return nil, fmt.Errorf("playbook metadata.name is required")
	}
	if len(playbook.Spec) == 0 || string(playbook.Spec) == "null" {
		return nil, fmt.Errorf("playbook spec is required")
	}

	var spec clientapi.PlaybookSpecSummary
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return nil, fmt.Errorf("invalid playbook spec: %w", err)
	}
	namespace := playbook.Metadata.Namespace
	if namespace == "" {
		namespace = "default"
	}
	return &sdk.PlaybookApplyParams{
		Namespace: namespace,
		Name:      playbook.Metadata.Name,
		Spec:      playbook.Spec,
	}, nil
}

func init() {
	ApplyPlaybook.Flags().StringVarP(&playbookApplyFile, "file", "f", "", "Playbook manifest to apply; use - for stdin")
	_ = ApplyPlaybook.MarkFlagRequired("file")
	Playbook.AddCommand(ApplyPlaybook)
}
