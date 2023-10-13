package actions

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/k8s"
)

const (
	defaultContainerTimeout = 30 * time.Minute

	playbookPodActionLabelPrefix = "playbooks.mission-control.flanksource.com"
)

type PodResult struct {
	Logs string
}

type Pod struct {
	PlaybookRun models.PlaybookRun
}

func (c *Pod) Run(ctx api.Context, action v1.PodAction, env TemplateEnv) (*PodResult, error) {
	timeout := time.Duration(action.Timeout) * time.Minute
	if timeout == 0 {
		timeout = defaultContainerTimeout
	}

	pod, err := newPod(ctx, action, c.PlaybookRun)
	if err != nil {
		return nil, fmt.Errorf("error creating pod struct: %w", err)
	}

	if _, err := ctx.Kubernetes().CoreV1().Pods(ctx.Namespace()).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("error creating pod: %w", err)
	}
	defer deletePod(ctx, pod)

	if err := k8s.WaitForPod(ctx, pod.Name, timeout, corev1.PodRunning, corev1.PodSucceeded, corev1.PodFailed); err != nil {
		return nil, fmt.Errorf("error waiting for pod to complete: %w", err)
	}

	return &PodResult{
		Logs: getLogs(ctx, pod, action.MaxLength),
	}, nil
}

func newPod(ctx api.Context, action v1.PodAction, playbookRun models.PlaybookRun) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	pod.Name = fmt.Sprintf("%s-%s", action.Name, playbookRun.ID.String())
	pod.Namespace = ctx.Namespace()
	pod.APIVersion = corev1.SchemeGroupVersion.Version
	pod.Labels = map[string]string{
		newPodLabel("pod-action"):    "true",
		newPodLabel("action"):        fmt.Sprintf("pod-action-%s-%s", action.Name, ctx.Namespace()),
		newPodLabel("playbookRunID"): playbookRun.ID.String(),
		newPodLabel("playbookID"):    playbookRun.PlaybookID.String(),
	}
	if err := json.Unmarshal(action.Spec, &pod.Spec); err != nil {
		return nil, fmt.Errorf("error unmarshalling pod spec: %w", err)
	}
	pod.Spec.RestartPolicy = corev1.RestartPolicyNever
	return pod, nil
}

func deletePod(ctx api.Context, pod *corev1.Pod) {
	if err := k8s.DeletePod(ctx, pod.Name); err != nil {
		logger.Warnf("failed to delete pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}
}

func getLogs(ctx api.Context, pod *corev1.Pod, maxLength int) string {
	message, _ := k8s.GetPodLogs(ctx, pod.Name, pod.Spec.Containers[0].Name)
	if maxLength > 0 {
		message = message[len(message)-maxLength:]
	}

	return message
}

func newPodLabel(key string) string {
	return fmt.Sprintf("%s/%s", playbookPodActionLabelPrefix, key)
}
