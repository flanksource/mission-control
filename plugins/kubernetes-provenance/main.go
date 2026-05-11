package main

import (
	"context"
	"encoding/json"
	"fmt"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/sdk"
)

var (
	Version   = ""
	BuildDate = ""
)

func main() {
	sdk.Serve(&KubernetesProvenancePlugin{})
}

type KubernetesProvenancePlugin struct{}

func (p *KubernetesProvenancePlugin) Manifest() *pluginpb.PluginManifest {
	return &pluginpb.PluginManifest{
		Name:         "kubernetes-provenance",
		Version:      sdk.FormatVersion(Version, BuildDate),
		Description:  "Explain Kubernetes object provenance: runtime owners, GitOps source, renderers, and field writers.",
		Capabilities: []string{"operations"},
	}
}

func (p *KubernetesProvenancePlugin) Configure(_ context.Context, _ map[string]any) error {
	return nil
}

func (p *KubernetesProvenancePlugin) Operations() []sdk.Operation {
	return []sdk.Operation{
		{
			Def: &pluginpb.OperationDef{
				Name:        "explain",
				Description: "Explain Kubernetes object provenance for the selected catalog item.",
				Scope:       "config",
				ResultMime:  sdk.ClickyResultMimeType,
			},
			Handler: p.explain,
		},
	}
}

type ExplainParams struct {
	Detectors            []string `json:"detectors,omitempty"`
	IncludeEvidence      *bool    `json:"includeEvidence,omitempty"`
	IncludeManagedFields *bool    `json:"includeManagedFields,omitempty"`
	MaxOwnerDepth        int      `json:"maxOwnerDepth,omitempty"`
}

func (p *KubernetesProvenancePlugin) explain(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	if req.ConfigItemID == "" {
		return nil, fmt.Errorf("config_id is required")
	}
	var params ExplainParams
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
			return nil, fmt.Errorf("decode params: %w", err)
		}
	}
	if params.MaxOwnerDepth <= 0 {
		params.MaxOwnerDepth = 5
	}
	includeEvidence := true
	if params.IncludeEvidence != nil {
		includeEvidence = *params.IncludeEvidence
	}
	includeManagedFields := true
	if params.IncludeManagedFields != nil {
		includeManagedFields = *params.IncludeManagedFields
	}

	cli, err := newKubeClient(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	item, err := req.Host.GetConfigItem(ctx, req.ConfigItemID)
	if err != nil {
		return nil, fmt.Errorf("get config item: %w", err)
	}
	obj, target, err := cli.getObjectForConfig(ctx, item)
	if err != nil {
		return nil, err
	}

	report := &ExplainReport{Target: target}
	runDetectors(ctx, cli, obj, params, includeManagedFields, report)
	report.pickSummary()
	if !includeEvidence {
		report.Evidence = nil
	}
	return report, nil
}
