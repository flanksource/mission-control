package main

import (
	"context"
	"fmt"
	"strings"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/sdk"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

type RunningPod struct {
	Namespace  string            `json:"namespace"`
	Name       string            `json:"name"`
	Node       string            `json:"node,omitempty"`
	Containers []string          `json:"containers"`
	OwnerKind  string            `json:"ownerKind,omitempty"`
	OwnerName  string            `json:"ownerName,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

type TargetRef struct {
	Namespace string            `json:"namespace"`
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Selector  map[string]string `json:"selector,omitempty"`
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
	pods, selector, err := podsForTarget(ctx, cli, target)
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
			Node:       p.Spec.NodeName,
			Containers: containerNames(p),
			Labels:     p.Labels,
		}
		if owner := controllerOwner(p); owner != "" {
			kind, name, _ := strings.Cut(owner, "/")
			row.OwnerKind = kind
			row.OwnerName = name
		}
		if len(selector) > 0 && row.Labels == nil {
			row.Labels = selector
		}
		out = append(out, row)
	}
	return out, nil
}

func podsForTarget(ctx context.Context, cli kubernetes.Interface, target TargetRef) ([]corev1.Pod, map[string]string, error) {
	switch normalizeKind(target.Kind) {
	case "pod", "pods", "po":
		pod, err := cli.CoreV1().Pods(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil, nil
		}
		if err != nil {
			return nil, nil, err
		}
		return []corev1.Pod{*pod}, pod.Labels, nil
	case "deployment", "deployments", "deploy":
		dep, err := cli.AppsV1().Deployments(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, err
		}
		sel := dep.Spec.Selector.MatchLabels
		pods, err := podsBySelector(ctx, cli, target.Namespace, sel)
		return pods, sel, err
	case "statefulset", "statefulsets", "sts":
		ss, err := cli.AppsV1().StatefulSets(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, err
		}
		sel := ss.Spec.Selector.MatchLabels
		pods, err := podsBySelector(ctx, cli, target.Namespace, sel)
		return pods, sel, err
	case "daemonset", "daemonsets", "ds":
		ds, err := cli.AppsV1().DaemonSets(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, err
		}
		sel := ds.Spec.Selector.MatchLabels
		pods, err := podsBySelector(ctx, cli, target.Namespace, sel)
		return pods, sel, err
	case "replicaset", "replicasets", "rs":
		rs, err := cli.AppsV1().ReplicaSets(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, err
		}
		sel := rs.Spec.Selector.MatchLabels
		pods, err := podsBySelector(ctx, cli, target.Namespace, sel)
		return pods, sel, err
	case "job", "jobs":
		job, err := cli.BatchV1().Jobs(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, err
		}
		if job.Spec.Selector == nil {
			return nil, nil, fmt.Errorf("job %s/%s has no selector", target.Namespace, target.Name)
		}
		sel := job.Spec.Selector.MatchLabels
		pods, err := podsBySelector(ctx, cli, target.Namespace, sel)
		return pods, sel, err
	case "cronjob", "cronjobs":
		jobs, err := cli.BatchV1().Jobs(target.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, nil, err
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
		return pods, nil, nil
	default:
		sel := map[string]string{"app": target.Name}
		pods, err := podsBySelector(ctx, cli, target.Namespace, sel)
		return pods, sel, err
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
