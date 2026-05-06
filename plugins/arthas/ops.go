package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/sdk"
	"github.com/flanksource/incident-commander/plugins/arthas/internal/arthas"
	arthask8s "github.com/flanksource/incident-commander/plugins/arthas/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

type RunningPod struct {
	Namespace  string   `json:"namespace"`
	Name       string   `json:"name"`
	Containers []string `json:"containers"`
	OwnerKind  string   `json:"ownerKind,omitempty"`
	OwnerName  string   `json:"ownerName,omitempty"`
}

type TargetRef struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
}

type SessionCreateParams struct {
	Namespace      string `json:"namespace,omitempty"`
	Kind           string `json:"kind,omitempty"`
	Name           string `json:"name,omitempty"`
	Pod            string `json:"pod,omitempty"`
	Container      string `json:"container,omitempty"`
	LocalHTTP      int    `json:"localHttp,omitempty"`
	LocalMCP       int    `json:"localMcp,omitempty"`
	RemoteHTTP     int    `json:"remoteHttp,omitempty"`
	RemoteMCP      int    `json:"remoteMcp,omitempty"`
	SkipJDKInstall bool   `json:"skipJdkInstall,omitempty"`
}

type SessionDeleteParams struct {
	ID string `json:"id"`
}

type ExecParams struct {
	SessionID string `json:"sessionId"`
	Command   string `json:"command"`
}

type execEnvelope struct {
	State string `json:"state"`
	Body  struct {
		State   string `json:"state"`
		Results []any  `json:"results"`
		Message string `json:"message"`
	} `json:"body"`
}

type ExecResponse struct {
	SessionID  string        `json:"sessionId"`
	Command    string        `json:"command"`
	State      string        `json:"state"`
	Results    []any         `json:"results,omitempty"`
	Message    string        `json:"message,omitempty"`
	DurationMS int64         `json:"durationMs"`
	Duration   time.Duration `json:"-"`
}

func (p *ArthasPlugin) sessionsList(_ context.Context, _ sdk.InvokeCtx) (any, error) {
	return p.sessions.List(), nil
}

func (p *ArthasPlugin) podsList(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	target, err := targetFromConfig(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return nil, err
	}
	cli, err := p.clients.Client(ctx, req.Host)
	if err != nil {
		return nil, err
	}
	return listRunningPodsForTarget(ctx, cli, target)
}

func (p *ArthasPlugin) sessionCreate(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params SessionCreateParams
	if len(req.ParamsJSON) > 0 {
		if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
			return nil, fmt.Errorf("decode params: %w", err)
		}
	}

	target, err := p.createTarget(ctx, req, params)
	if err != nil {
		return nil, err
	}

	cfg, err := p.clients.RESTConfig(ctx, req.Host)
	if err != nil {
		return nil, err
	}
	pod, container, err := arthask8s.ResolvePod(ctx, cfg, target.Namespace, target.Kind, target.Name, params.Container)
	if err != nil {
		return nil, fmt.Errorf("resolve pod: %w", err)
	}

	startCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	sess, err := arthas.Start(startCtx, cfg, arthas.StartOptions{
		Namespace:      target.Namespace,
		Kind:           target.Kind,
		Name:           target.Name,
		Pod:            pod,
		Container:      container,
		LocalHTTP:      params.LocalHTTP,
		LocalMCP:       params.LocalMCP,
		RemoteHTTP:     params.RemoteHTTP,
		RemoteMCP:      params.RemoteMCP,
		SkipJDKInstall: params.SkipJDKInstall,
	})
	if err != nil {
		return nil, fmt.Errorf("start arthas: %w", err)
	}
	p.sessions.Add(sess)
	return sess, nil
}

func (p *ArthasPlugin) createTarget(ctx context.Context, req sdk.InvokeCtx, params SessionCreateParams) (TargetRef, error) {
	if params.Pod != "" {
		ns := params.Namespace
		if ns == "" {
			base, err := targetFromConfig(ctx, req.Host, req.ConfigItemID)
			if err != nil {
				return TargetRef{}, err
			}
			ns = base.Namespace
		}
		return TargetRef{Namespace: ns, Kind: "pod", Name: params.Pod}, nil
	}
	if params.Kind != "" && params.Name != "" && params.Namespace != "" {
		return TargetRef{Namespace: params.Namespace, Kind: normalizeKind(params.Kind), Name: params.Name}, nil
	}
	base, err := targetFromConfig(ctx, req.Host, req.ConfigItemID)
	if err != nil {
		return TargetRef{}, err
	}
	if params.Container != "" {
		return base, nil
	}
	if params.Kind != "" && params.Name != "" {
		base.Kind = normalizeKind(params.Kind)
		base.Name = params.Name
	}
	if params.Namespace != "" {
		base.Namespace = params.Namespace
	}
	return base, nil
}

func (p *ArthasPlugin) sessionDelete(_ context.Context, req sdk.InvokeCtx) (any, error) {
	var params SessionDeleteParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if params.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	removed, err := p.sessions.Remove(params.ID)
	if !removed {
		return nil, fmt.Errorf("session %q not found", params.ID)
	}
	return map[string]any{"deleted": true, "id": params.ID}, err
}

func (p *ArthasPlugin) exec(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	var params ExecParams
	if err := json.Unmarshal(req.ParamsJSON, &params); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	if params.SessionID == "" || params.Command == "" {
		return nil, fmt.Errorf("sessionId and command are required")
	}
	return p.execCommand(ctx, params.SessionID, params.Command)
}

func (p *ArthasPlugin) execCommand(ctx context.Context, sessionID, command string) (ExecResponse, error) {
	sess, ok := p.sessions.Get(sessionID)
	if !ok {
		return ExecResponse{}, fmt.Errorf("session %q not found", sessionID)
	}
	body, err := json.Marshal(map[string]any{"action": "exec", "command": command})
	if err != nil {
		return ExecResponse{}, err
	}
	started := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/api", sess.HTTPLocalPort), bytes.NewReader(body))
	if err != nil {
		return ExecResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(httpReq)
	if err != nil {
		return ExecResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return ExecResponse{}, readHTTPError(resp)
	}
	var envelope execEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return ExecResponse{}, fmt.Errorf("decode arthas response: %w", err)
	}
	state := envelope.Body.State
	if state == "" {
		state = envelope.State
	}
	d := time.Since(started)
	out := ExecResponse{
		SessionID:  sessionID,
		Command:    command,
		State:      state,
		Results:    envelope.Body.Results,
		Message:    envelope.Body.Message,
		Duration:   d,
		DurationMS: d.Milliseconds(),
	}
	if !strings.EqualFold(state, "SUCCEEDED") {
		return out, fmt.Errorf("arthas command failed (%s): %s", state, out.Message)
	}
	return out, nil
}

func targetFromConfig(ctx context.Context, host sdk.HostClient, configID string) (TargetRef, error) {
	if host == nil || configID == "" {
		return TargetRef{}, fmt.Errorf("config_id is required")
	}
	item, err := host.GetConfigItem(ctx, configID)
	if err != nil {
		return TargetRef{}, fmt.Errorf("get config item: %w", err)
	}
	kind, namespace, name := extractKubeRef(item)
	if name == "" {
		return TargetRef{}, fmt.Errorf("config item %s has no Kubernetes name", configID)
	}
	if namespace == "" {
		namespace = "default"
	}
	return TargetRef{Namespace: namespace, Kind: kind, Name: name}, nil
}

func extractKubeRef(item *pluginpb.ConfigItem) (kind, namespace, name string) {
	kind = normalizeKind(item.Type)
	namespace = item.Tags["namespace"]
	if namespace == "" {
		namespace = item.Namespace
	}
	name = item.Name
	return
}

func normalizeKind(kind string) string {
	kind = strings.TrimSpace(kind)
	kind = strings.TrimPrefix(kind, "Kubernetes::")
	return strings.ToLower(kind)
}

func listRunningPodsForTarget(ctx context.Context, cli kubernetes.Interface, target TargetRef) ([]RunningPod, error) {
	pods, err := podsForTarget(ctx, cli, target)
	if err != nil {
		return nil, err
	}
	out := make([]RunningPod, 0, len(pods))
	for i := range pods {
		p := pods[i]
		if p.Status.Phase != corev1.PodRunning || !hasReadyCondition(p) {
			continue
		}
		row := RunningPod{
			Namespace:  p.Namespace,
			Name:       p.Name,
			Containers: containerNames(p),
		}
		if owner := controllerOwner(p); owner != "" {
			kind, name, _ := strings.Cut(owner, "/")
			row.OwnerKind = kind
			row.OwnerName = name
		}
		out = append(out, row)
	}
	return out, nil
}

func podsForTarget(ctx context.Context, cli kubernetes.Interface, target TargetRef) ([]corev1.Pod, error) {
	switch normalizeKind(target.Kind) {
	case "pod", "pods", "po":
		pod, err := cli.CoreV1().Pods(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return []corev1.Pod{*pod}, nil
	case "deployment", "deployments", "deploy":
		dep, err := cli.AppsV1().Deployments(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return podsBySelector(ctx, cli, target.Namespace, dep.Spec.Selector.MatchLabels)
	case "statefulset", "statefulsets", "sts":
		ss, err := cli.AppsV1().StatefulSets(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return podsBySelector(ctx, cli, target.Namespace, ss.Spec.Selector.MatchLabels)
	case "daemonset", "daemonsets", "ds":
		ds, err := cli.AppsV1().DaemonSets(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return podsBySelector(ctx, cli, target.Namespace, ds.Spec.Selector.MatchLabels)
	case "replicaset", "replicasets", "rs":
		rs, err := cli.AppsV1().ReplicaSets(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return podsBySelector(ctx, cli, target.Namespace, rs.Spec.Selector.MatchLabels)
	case "job", "jobs":
		job, err := cli.BatchV1().Jobs(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return podsBySelector(ctx, cli, target.Namespace, job.Spec.Selector.MatchLabels)
	case "cronjob", "cronjobs":
		jobs, err := cli.BatchV1().Jobs(target.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		var pods []corev1.Pod
		for _, job := range jobs.Items {
			if !ownedBy(job.OwnerReferences, "CronJob", target.Name) || job.Spec.Selector == nil {
				continue
			}
			ps, err := podsBySelector(ctx, cli, target.Namespace, job.Spec.Selector.MatchLabels)
			if err != nil {
				continue
			}
			pods = append(pods, ps...)
		}
		return pods, nil
	default:
		return podsBySelector(ctx, cli, target.Namespace, map[string]string{"app": target.Name})
	}
}

func podsBySelector(ctx context.Context, cli kubernetes.Interface, namespace string, sel map[string]string) ([]corev1.Pod, error) {
	if len(sel) == 0 {
		return nil, fmt.Errorf("empty selector")
	}
	list, err := cli.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(sel).String(),
	})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func hasReadyCondition(p corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func containerNames(p corev1.Pod) []string {
	names := make([]string, 0, len(p.Spec.Containers))
	for _, c := range p.Spec.Containers {
		names = append(names, c.Name)
	}
	return names
}

func controllerOwner(p corev1.Pod) string {
	for _, o := range p.OwnerReferences {
		if o.Controller == nil || !*o.Controller {
			continue
		}
		kind := strings.ToLower(o.Kind)
		name := o.Name
		switch kind {
		case "replicaset":
			if idx := strings.LastIndex(name, "-"); idx > 0 {
				return "deployment/" + name[:idx]
			}
			return "replicaset/" + name
		case "deployment", "statefulset", "daemonset", "job":
			return kind + "/" + name
		}
	}
	return ""
}

func ownedBy(refs []metav1.OwnerReference, kind, name string) bool {
	for _, r := range refs {
		if r.Kind == kind && r.Name == name {
			return true
		}
	}
	return false
}

func readHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	text := strings.TrimSpace(string(body))
	if text == "" {
		text = resp.Status
	}
	return fmt.Errorf("%s: %s", resp.Status, text)
}
