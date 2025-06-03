package k8s

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/flanksource/incident-commander/logs"
)

// Request represents available parameters for Kubernetes log queries.
//
// +kubebuilder:object:generate=true
type Request struct {
	logs.LogsRequestBase `json:",inline" yaml:",inline" template:"true"`

	Kind       string `json:"kind" template:"true"`
	ApiVersion string `json:"apiVersion" template:"true"`
	Namespace  string `json:"namespace" template:"true"`
	Name       string `json:"name" template:"true"`

	// Logs will include pods that match any of these selectors.
	//
	// This applies when retrieving logs at a higher resource level,
	// such as fetching logs for a deployment spanning multiple pods.
	Pods types.ResourceSelectors `json:"pods,omitempty"`

	// Containers filters logs from only these containers.
	Containers types.MatchExpressions `json:"containers,omitempty"`
}

type K8sLogFetcher struct {
	conn connection.KubernetesConnection
}

func NewK8sLogsFetcher(conn connection.KubernetesConnection) *K8sLogFetcher {
	return &K8sLogFetcher{
		conn: conn,
	}
}

func (t *K8sLogFetcher) Fetch(ctx context.Context, request Request) ([]logs.LogResult, error) {
	client, _, err := t.conn.Populate(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("failed to populate kubernetes connection: %w", err)
	}

	switch request.Kind {
	case "Pod":
		pod, err := client.CoreV1().Pods(request.Namespace).Get(ctx, request.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get pod %s/%s: %w", request.Namespace, request.Name, err)
		}

		return fetchPodLogs(ctx, client, *pod, request)

	case "Deployment":
		return fetchDeploymentLogs(ctx, client, request.Namespace, request.Name, request)

	case "StatefulSet":
		return fetchStatefulSetLogs(ctx, client, request.Namespace, request.Name, request)

	case "DaemonSet":
		return fetchDaemonSetLogs(ctx, client, request.Namespace, request.Name, request)
	}

	return nil, fmt.Errorf("unsupported kind: %s", request.Kind)
}

func fetchStatefulSetLogs(ctx context.Context, client kubernetes.Interface, namespace, name string, request Request) ([]logs.LogResult, error) {
	sts, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get StatefulSet %s/%s: %w", namespace, name, err)
	}

	selector := metav1.FormatLabelSelector(sts.Spec.Selector)
	return fetchPodsLogs(ctx, client, selector, request)
}

func fetchDaemonSetLogs(ctx context.Context, client kubernetes.Interface, namespace, name string, request Request) ([]logs.LogResult, error) {
	ds, err := client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get DaemonSet %s/%s: %w", namespace, name, err)
	}

	selector := metav1.FormatLabelSelector(ds.Spec.Selector)
	return fetchPodsLogs(ctx, client, selector, request)
}

func fetchDeploymentLogs(ctx context.Context, client kubernetes.Interface, namespace, name string, request Request) ([]logs.LogResult, error) {
	deploy, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Deployment %s/%s: %w", namespace, name, err)
	}

	selector := metav1.FormatLabelSelector(deploy.Spec.Selector)
	return fetchPodsLogs(ctx, client, selector, request)
}

func fetchPodsLogs(ctx context.Context, client kubernetes.Interface, selector string, request Request) ([]logs.LogResult, error) {
	podList, err := client.CoreV1().Pods(request.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods in namespace %q with selector %q: %w", request.Namespace, selector, err)
	}

	var logGroups []logs.LogResult
	for _, pod := range podList.Items {
		if logs, err := fetchPodLogs(ctx, client, pod, request); err != nil {
			return nil, err
		} else if logs != nil {
			logGroups = append(logGroups, logs...)
		}
	}

	return logGroups, nil
}

func fetchPodLogs(ctx context.Context, client kubernetes.Interface, pod corev1.Pod, request Request) ([]logs.LogResult, error) {
	if len(request.Pods) > 0 {
		// Convert the pod to config item so it's resource selectable
		// Instead of resource selectors maybe we can just use a cel-expression?
		configItem := models.ConfigItem{
			Labels: lo.ToPtr(types.JSONStringMap(pod.Labels)),
			Name:   &pod.Name,
			Tags: map[string]string{
				"namespace": pod.Namespace,
			},
		}
		if !request.Pods.Matches(configItem) {
			return nil, nil
		}
	}

	var logGroups []logs.LogResult
	if len(request.Containers) == 0 {
		if logs, err := fetchContainerLogs(ctx, client, pod, "", request); err != nil {
			return nil, err
		} else if logs != nil {
			logGroups = append(logGroups, *logs)
		}
	} else {
		for _, container := range pod.Spec.Containers {
			if request.Containers.Match(container.Name) {
				if logs, err := fetchContainerLogs(ctx, client, pod, container.Name, request); err != nil {
					return nil, err
				} else if logs != nil {
					logGroups = append(logGroups, *logs)
				}
			}
		}
	}

	return logGroups, nil
}

func fetchContainerLogs(ctx context.Context, client kubernetes.Interface, pod corev1.Pod, containerName string, request Request) (*logs.LogResult, error) {
	opt := &corev1.PodLogOptions{
		Container:  containerName,
		Timestamps: true,
	}

	if s, err := request.GetStart(); err == nil {
		opt.SinceTime = &metav1.Time{Time: s}
	}

	if request.Limit != "" {
		limit, err := strconv.ParseInt(request.Limit, 10, 32)
		if err != nil {
			return nil, err
		}
		opt.TailLines = lo.ToPtr(limit)
	}

	req := client.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, opt)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer podLogs.Close()

	var output logs.LogResult
	scanner := bufio.NewScanner(podLogs)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), " ", 2)
		if len(parts) < 2 {
			continue
		}

		t, err := time.Parse(time.RFC3339, parts[0])
		if err != nil {
			continue
		}

		line := &logs.LogLine{
			Count:         1,
			Message:       parts[1],
			Labels:        pod.Labels,
			Host:          pod.Name,
			FirstObserved: t,
		}
		line.SetHash()
		output.Logs = append(output.Logs, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading logs: %w", err)
	}

	return &output, nil
}
