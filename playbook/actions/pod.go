package actions

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

const (
	defaultMaxLength        = 3000
	defaultContainerTimeout = 5 * time.Minute

	sidecarContainerName     = "container-waiter"
	containerImage           = "ubuntu:jammy"
	playbookActionSelector   = "playbooks.mission-control.flanksource.com/actions"
	playbookActionLabelValue = "playbooks-pod-action"
)

type PodResult struct {
	Logs string
}

type Pod struct {
}

func (c *Pod) Run(ctx api.Context, action v1.PodAction, env TemplateEnv) (*PodResult, error) {
	timeout := time.Duration(action.Timeout) * time.Minute
	if timeout == 0 {
		timeout = defaultContainerTimeout
	}

	if action.MaxLength <= 0 {
		action.MaxLength = defaultMaxLength
	}

	pod, err := newPod(ctx, action)
	if err != nil {
		return nil, err
	}

	if err := cleanupExistingPods(ctx, fmt.Sprintf("%s=%s", playbookActionLabelValue, pod.Labels[playbookActionLabelValue])); err != nil {
		return nil, err
	}

	if _, err := ctx.Kubernetes().CoreV1().Pods(ctx.Namespace()).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return nil, err
	}
	defer deletePod(ctx, pod)

	if err := ctx.Kommons().WaitForPod(ctx.Namespace(), pod.Name, timeout, corev1.PodRunning, corev1.PodSucceeded, corev1.PodFailed); err != nil {
		return nil, err
	}

	return &PodResult{
		Logs: getLogs(ctx, pod, action.MaxLength),
	}, nil
}

func newPod(ctx api.Context, action v1.PodAction) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	pod.APIVersion = corev1.SchemeGroupVersion.Version
	pod.Labels = map[string]string{
		playbookActionSelector: getPlaybookActionLabel(playbookActionLabelValue, action.Name, ctx.Namespace()),
	}
	pod.Namespace = ctx.Namespace()
	pod.Name = action.Name + "-" + strings.ToLower(rand.String(5))
	if err := json.Unmarshal(action.Spec, &pod.Spec); err != nil {
		return nil, err
	}
	pod.Spec.RestartPolicy = corev1.RestartPolicyNever
	return pod, nil
}

func deletePod(ctx api.Context, pod *corev1.Pod) {
	if err := ctx.Kommons().DeleteByKind("Pod", pod.Namespace, pod.Name); err != nil {
		logger.Warnf("failed to delete pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}
}

func getLogs(ctx api.Context, pod *corev1.Pod, maxLength int) string {
	message, _ := ctx.Kommons().GetPodLogs(pod.Namespace, pod.Name, pod.Spec.Containers[0].Name)
	if len(message) > maxLength {
		message = message[len(message)-maxLength:]
	}

	return message
}

func getPlaybookActionLabel(label, name, namespace string) string {
	return fmt.Sprintf("%v-%v-%v", label, name, namespace)
}

func cleanupExistingPods(ctx api.Context, selector string) error {
	pods := ctx.Kubernetes().CoreV1().Pods(ctx.Namespace())
	existingPods, err := pods.List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return err
	}

	for _, junitPod := range existingPods.Items {
		deletePod(ctx, &junitPod)
	}

	return nil
}
