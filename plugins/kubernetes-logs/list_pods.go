package main

import (
	"context"
	"fmt"

	"github.com/flanksource/incident-commander/plugin/sdk"
)

func (p *KubernetesLogsPlugin) listPods(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	cli, err := p.clients.For(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	pods, err := resolvePods(ctx, cli, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	type Row struct {
		Namespace string `json:"namespace"`
		Pod       string `json:"pod"`
		Phase     string `json:"phase"`
		OwnedBy   string `json:"ownedBy,omitempty"`
	}
	out := make([]Row, 0, len(pods))
	for _, pod := range pods {
		owned := ""
		if len(pod.OwnerReferences) > 0 {
			owned = fmt.Sprintf("%s/%s", pod.OwnerReferences[0].Kind, pod.OwnerReferences[0].Name)
		}
		out = append(out, Row{
			Namespace: pod.Namespace,
			Pod:       pod.Name,
			Phase:     string(pod.Status.Phase),
			OwnedBy:   owned,
		})
	}
	return out, nil
}
