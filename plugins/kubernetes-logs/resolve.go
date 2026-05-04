package main

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/sdk"
)

// resolvePods turns a config item into the set of pods we should fetch logs
// from. Mission-control tags Kubernetes resources with `name`, `namespace`,
// and the resource Kind in tags/labels. We use the host's GetConfigItem
// to read those, then walk down to pods according to the workload kind.
//
// Supported kinds (case-insensitive):
//
//   - Pod                 — itself
//   - Deployment          — via ReplicaSet selector
//   - StatefulSet         — via selector
//   - DaemonSet           — via selector
//   - ReplicaSet          — via selector
//   - Job / CronJob       — via selector
//
// Any other kind falls back to "label match by app=<name> in <namespace>",
// which covers the typical Helm chart layout without per-controller
// custom logic.
func resolvePods(ctx context.Context, cli kubernetes.Interface, host sdk.HostClient, configItemID string) ([]corev1.Pod, error) {
	if host == nil || configItemID == "" {
		return nil, fmt.Errorf("config_item_id is required")
	}
	item, err := host.GetConfigItem(ctx, configItemID)
	if err != nil {
		return nil, fmt.Errorf("get config item: %w", err)
	}

	kind, namespace, name := extractKubeRef(item)
	if name == "" {
		return nil, fmt.Errorf("config item %s has no Kubernetes name tag", configItemID)
	}
	if namespace == "" {
		namespace = "default"
	}

	switch strings.ToLower(kind) {
	case "pod", "kubernetes::pod":
		pod, err := cli.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return []corev1.Pod{*pod}, nil

	case "deployment", "kubernetes::deployment":
		dep, err := cli.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return podsBySelector(ctx, cli, namespace, dep.Spec.Selector.MatchLabels)

	case "statefulset", "kubernetes::statefulset":
		ss, err := cli.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return podsBySelector(ctx, cli, namespace, ss.Spec.Selector.MatchLabels)

	case "daemonset", "kubernetes::daemonset":
		ds, err := cli.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return podsBySelector(ctx, cli, namespace, ds.Spec.Selector.MatchLabels)

	case "replicaset", "kubernetes::replicaset":
		rs, err := cli.AppsV1().ReplicaSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return podsBySelector(ctx, cli, namespace, rs.Spec.Selector.MatchLabels)

	case "job", "kubernetes::job":
		job, err := cli.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return podsBySelector(ctx, cli, namespace, job.Spec.Selector.MatchLabels)

	case "cronjob", "kubernetes::cronjob":
		// Find every Job owned by the CronJob, then every Pod owned by those Jobs.
		jobs, err := cli.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		} else if err != nil {
			return nil, err
		}
		var pods []corev1.Pod
		for _, j := range jobs.Items {
			if !ownedBy(j.OwnerReferences, "CronJob", name) {
				continue
			}
			ps, err := podsBySelector(ctx, cli, namespace, j.Spec.Selector.MatchLabels)
			if err != nil {
				continue
			}
			pods = append(pods, ps...)
		}
		return pods, nil
	}

	// Fallback: try `app=<name>` (the dominant Helm convention).
	return podsBySelector(ctx, cli, namespace, map[string]string{"app": name})
}

// extractKubeRef pulls the Kubernetes kind / namespace / name from a config
// item. Mission-control's Kubernetes scraper writes these as tags.
func extractKubeRef(item *pluginpb.ConfigItem) (kind, namespace, name string) {
	kind = item.Type
	namespace = item.Tags["namespace"]
	name = item.Name
	return
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

func ownedBy(refs []metav1.OwnerReference, kind, name string) bool {
	for _, r := range refs {
		if r.Kind == kind && r.Name == name {
			return true
		}
	}
	return false
}
