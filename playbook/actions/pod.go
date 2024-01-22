package actions

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/flanksource/artifacts"
	fileUtils "github.com/flanksource/commons/files"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/k8s"
)

const (
	defaultContainerTimeout = 30 * time.Minute

	playbookPodActionLabelPrefix = "playbooks.mission-control.flanksource.com"
)

type PodResult struct {
	Logs string

	Artifacts []artifacts.Artifact `json:"-" yaml:"-"`
}

type Pod struct {
	PlaybookRunID uuid.UUID
	PlaybookID    uuid.UUID
}

func (c *Pod) Run(ctx context.Context, action v1.PodAction, timeout time.Duration) (*PodResult, error) {
	if timeout == 0 {
		timeout = defaultContainerTimeout
	}

	pod, err := newPod(ctx, action, c.PlaybookID, c.PlaybookRunID)
	if err != nil {
		return nil, fmt.Errorf("error creating pod struct: %w", err)
	}

	if _, err := ctx.Kubernetes().CoreV1().Pods(ctx.GetNamespace()).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("error creating pod: %w", err)
	}
	defer deletePod(ctx, pod)

	if err := k8s.WaitForPod(ctx, pod.Name, timeout, corev1.PodRunning, corev1.PodSucceeded, corev1.PodFailed); err != nil {
		return nil, fmt.Errorf("error waiting for pod to complete: %w", err)
	}

	output := &PodResult{
		Logs: getLogs(ctx, pod, action.MaxLength),
	}

	for _, artifactConfig := range action.Artifacts {
		paths, err := fileUtils.UnfoldGlobs(artifactConfig.Path)
		if err != nil {
			return nil, err
		}

		for _, path := range paths {
			file, err := os.Open(path)
			if err != nil {
				logger.Errorf("error opening file. path=%s; %w", path, err)
				continue
			}

			output.Artifacts = append(output.Artifacts, artifacts.Artifact{
				Path:    path,
				Content: file,
			})
		}
	}

	return output, nil
}

func newPod(ctx context.Context, action v1.PodAction, playbookID, runID uuid.UUID) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	pod.Name = fmt.Sprintf("%s-%s", action.Name, runID.String())
	pod.Namespace = ctx.GetNamespace()
	pod.APIVersion = corev1.SchemeGroupVersion.Version
	pod.Labels = map[string]string{
		newPodLabel("pod-action"):    "true",
		newPodLabel("action"):        fmt.Sprintf("pod-action-%s-%s", action.Name, ctx.GetNamespace()),
		newPodLabel("playbookRunID"): runID.String(),
		newPodLabel("playbookID"):    playbookID.String(),
	}
	if err := json.Unmarshal(action.Spec, &pod.Spec); err != nil {
		return nil, fmt.Errorf("error unmarshalling pod spec: %w", err)
	}
	pod.Spec.RestartPolicy = corev1.RestartPolicyNever
	return pod, nil
}

func deletePod(ctx context.Context, pod *corev1.Pod) {
	if err := k8s.DeletePod(ctx, pod.Name); err != nil {
		logger.Warnf("failed to delete pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}
}

func getLogs(ctx context.Context, pod *corev1.Pod, maxLength int) string {
	message, _ := k8s.GetPodLogs(ctx, pod.Name, pod.Spec.Containers[0].Name)
	if maxLength > 0 {
		message = message[len(message)-maxLength:]
	}

	return message
}

func newPodLabel(key string) string {
	return fmt.Sprintf("%s/%s", playbookPodActionLabelPrefix, key)
}
