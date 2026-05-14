package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	dutyContext "github.com/flanksource/duty/context"
	dutylogs "github.com/flanksource/duty/logs"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/plugin/sdk"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// TailParams is the input shape for the `tail` operation. The CLI sends
// these as JSON via --param/--json; the iframe sends them through the
// `tail` button. PostProcess mirrors the playbook `logs` action shape so
// CEL match expressions and dedup/window settings work the same way.
type TailParams struct {
	Container   string             `json:"container,omitempty"`
	TailLines   int64              `json:"tailLines,omitempty"`
	Previous    bool               `json:"previous,omitempty"`
	PostProcess v1.LogsPostProcess `json:"postProcess,omitempty"`
}

func (p *KubernetesLogsPlugin) tail(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params TailParams
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
			return nil, fmt.Errorf("decode params: %w", err)
		}
	}
	if params.TailLines <= 0 {
		params.TailLines = 200
	}

	cli, err := p.clients.For(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}

	pods, err := resolvePods(ctx, cli, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}

	var lines []*dutylogs.LogLine
	for _, pod := range pods {
		podLines, err := tailPod(ctx, cli, pod, params)
		if err != nil {
			lines = append(lines, errorLine(pod, "", err.Error()))
			continue
		}
		lines = append(lines, podLines...)
	}

	dctx := dutyContext.NewContext(ctx)
	return postProcessLogs(dctx, lines, params.PostProcess), nil
}

// tailPod returns the last params.TailLines log lines from every container
// in the pod (or just one if params.Container is set). Each returned LogLine
// has FirstObserved set when the kubelet provided a timestamp, plus
// pod/container labels so postProcessLogs can dedupe/filter on them.
func tailPod(ctx context.Context, cli kubernetes.Interface, pod corev1.Pod, params TailParams) ([]*dutylogs.LogLine, error) {
	containers := containerNames(pod, params.Container)
	var out []*dutylogs.LogLine
	for _, name := range containers {
		opts := &corev1.PodLogOptions{
			Container:  name,
			TailLines:  &params.TailLines,
			Previous:   params.Previous,
			Timestamps: true,
		}
		req := cli.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, opts)
		stream, err := req.Stream(ctx)
		if err != nil {
			out = append(out, errorLine(pod, name, err.Error()))
			continue
		}
		lines, err := readLines(stream)
		_ = stream.Close()
		if err != nil {
			out = append(out, errorLine(pod, name, err.Error()))
			continue
		}
		for _, ln := range lines {
			out = append(out, parseKubeLogLine(pod, name, ln))
		}
	}
	return out, nil
}

// parseKubeLogLine splits the leading RFC3339Nano timestamp produced by
// PodLogOptions.Timestamps from the message body. If parsing fails, the
// whole line is treated as the message and FirstObserved is left zero.
func parseKubeLogLine(pod corev1.Pod, container, raw string) *dutylogs.LogLine {
	line := &dutylogs.LogLine{
		Host:   pod.Name,
		Source: container,
		Count:  1,
		Labels: map[string]string{
			"namespace": pod.Namespace,
			"pod":       pod.Name,
			"container": container,
		},
	}
	if idx := strings.IndexByte(raw, ' '); idx > 0 {
		if ts, err := time.Parse(time.RFC3339Nano, raw[:idx]); err == nil {
			line.FirstObserved = ts
			line.Message = raw[idx+1:]
			line.SetHash()
			return line
		}
	}
	line.Message = raw
	line.SetHash()
	return line
}

func errorLine(pod corev1.Pod, container, msg string) *dutylogs.LogLine {
	return &dutylogs.LogLine{
		Host:     pod.Name,
		Source:   container,
		Severity: "error",
		Message:  "ERROR: " + msg,
		Count:    1,
		Labels: map[string]string{
			"namespace": pod.Namespace,
			"pod":       pod.Name,
			"container": container,
		},
	}
}

// containerNames returns the containers to pull logs from. When override is
// empty, every container in the pod is included.
func containerNames(pod corev1.Pod, override string) []string {
	if override != "" {
		return []string{override}
	}
	names := make([]string, 0, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		names = append(names, c.Name)
	}
	return names
}

func readLines(r io.Reader) ([]string, error) {
	var out []string
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		out = append(out, strings.TrimRight(scanner.Text(), "\r"))
	}
	return out, scanner.Err()
}
