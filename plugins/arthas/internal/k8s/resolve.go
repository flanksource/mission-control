package k8s

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ParseKindName splits "kind/name" into its parts. Kind is lower-cased for
// comparison. Returns an error for malformed input.
func ParseKindName(target string) (kind, name string, err error) {
	parts := strings.SplitN(target, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected kind/name (e.g. deployment/foo), got %q", target)
	}
	return strings.ToLower(parts[0]), parts[1], nil
}

// ResolvePod turns a kind/name reference into a specific pod name, plus the
// container to target. Supported kinds: pod, deployment, statefulset,
// daemonset, replicaset, job, and cronjob.
// If preferredContainer is empty, the first container in the pod spec is used.
func ResolvePod(ctx context.Context, restCfg *rest.Config, namespace, kind, name, preferredContainer string) (pod, container string, err error) {
	if namespace == "" {
		return "", "", fmt.Errorf("namespace is required")
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return "", "", fmt.Errorf("build kube client: %w", err)
	}

	selector, err := selectorForController(ctx, cs, namespace, kind, name)
	if err != nil {
		return "", "", err
	}

	var podObj *corev1.Pod
	if selector == "" {
		// Direct pod reference.
		p, err := cs.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return "", "", fmt.Errorf("pod %s/%s not found", namespace, name)
			}
			return "", "", fmt.Errorf("get pod: %w", err)
		}
		podObj = p
	} else {
		pods, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return "", "", fmt.Errorf("list pods for %s/%s: %w", kind, name, err)
		}
		podObj = pickReadyPod(pods.Items)
		if podObj == nil {
			return "", "", fmt.Errorf("no Ready pods found for %s/%s in %s (selector %q)", kind, name, namespace, selector)
		}
	}

	return podObj.Name, chooseContainer(podObj, preferredContainer), nil
}

func ImageDigest(ctx context.Context, restCfg *rest.Config, namespace, podName, container string) (string, error) {
	if namespace == "" {
		return "", fmt.Errorf("namespace is required")
	}
	if podName == "" {
		return "", fmt.Errorf("pod is required")
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return "", fmt.Errorf("build kube client: %w", err)
	}
	pod, err := cs.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get pod %s/%s: %w", namespace, podName, err)
	}
	statuses := append([]corev1.ContainerStatus{}, pod.Status.ContainerStatuses...)
	statuses = append(statuses, pod.Status.InitContainerStatuses...)
	for _, status := range statuses {
		if container != "" && status.Name != container {
			continue
		}
		digest := normalizeImageID(status.ImageID)
		if digest != "" {
			return digest, nil
		}
	}
	if container != "" {
		return "", fmt.Errorf("container %q in pod %s/%s has no image digest", container, namespace, podName)
	}
	return "", fmt.Errorf("pod %s/%s has no container image digest", namespace, podName)
}

func normalizeImageID(imageID string) string {
	imageID = strings.TrimSpace(imageID)
	if imageID == "" {
		return ""
	}
	if i := strings.LastIndex(imageID, "://"); i >= 0 {
		imageID = imageID[i+3:]
	}
	if i := strings.LastIndex(imageID, "@"); i >= 0 {
		imageID = imageID[i+1:]
	}
	return imageID
}

func selectorForController(ctx context.Context, cs *kubernetes.Clientset, namespace, kind, name string) (string, error) {
	switch kind {
	case "pod", "pods", "po":
		return "", nil
	case "deployment", "deployments", "deploy":
		d, err := cs.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("get deployment %s/%s: %w", namespace, name, err)
		}
		return labels.Set(d.Spec.Selector.MatchLabels).String(), nil
	case "statefulset", "statefulsets", "sts":
		s, err := cs.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("get statefulset %s/%s: %w", namespace, name, err)
		}
		return labels.Set(s.Spec.Selector.MatchLabels).String(), nil
	case "daemonset", "daemonsets", "ds":
		d, err := cs.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("get daemonset %s/%s: %w", namespace, name, err)
		}
		return labels.Set(d.Spec.Selector.MatchLabels).String(), nil
	case "replicaset", "replicasets", "rs":
		rs, err := cs.AppsV1().ReplicaSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("get replicaset %s/%s: %w", namespace, name, err)
		}
		return labels.Set(rs.Spec.Selector.MatchLabels).String(), nil
	case "job", "jobs":
		job, err := cs.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("get job %s/%s: %w", namespace, name, err)
		}
		if job.Spec.Selector == nil {
			return "", fmt.Errorf("job %s/%s has no selector", namespace, name)
		}
		return labels.Set(job.Spec.Selector.MatchLabels).String(), nil
	case "cronjob", "cronjobs":
		jobs, err := cs.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return "", fmt.Errorf("list jobs for cronjob %s/%s: %w", namespace, name, err)
		}
		for _, job := range jobs.Items {
			if !ownedBy(job.OwnerReferences, "CronJob", name) || job.Spec.Selector == nil {
				continue
			}
			return labels.Set(job.Spec.Selector.MatchLabels).String(), nil
		}
		return "", fmt.Errorf("no jobs found for cronjob %s/%s", namespace, name)
	default:
		return "", fmt.Errorf("unsupported kind %q (want pod, deployment, statefulset, daemonset, replicaset, job, or cronjob)", kind)
	}
}

func ownedBy(refs []metav1.OwnerReference, kind, name string) bool {
	for _, r := range refs {
		if r.Kind == kind && r.Name == name {
			return true
		}
	}
	return false
}

func pickReadyPod(pods []corev1.Pod) *corev1.Pod {
	for i := range pods {
		p := &pods[i]
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				return p
			}
		}
	}
	return nil
}

func chooseContainer(p *corev1.Pod, preferred string) string {
	if preferred != "" {
		return preferred
	}
	if len(p.Spec.Containers) > 0 {
		return p.Spec.Containers[0].Name
	}
	return ""
}
