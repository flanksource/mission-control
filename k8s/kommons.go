package k8s

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/flanksource/duty/context"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WaitForPod waits for a pod to be in the specified phase, or returns an
// error if the timeout is exceeded
func WaitForPod(ctx context.Context, name string, timeout time.Duration, phases ...v1.PodPhase) error {
	kubeclient, err := ctx.Kubernetes()
	if err != nil {
		return fmt.Errorf("failed to get kubernetes client: %w", err)
	}

	pods := kubeclient.CoreV1().Pods(ctx.GetNamespace())
	start := time.Now()
	for {
		pod, err := pods.Get(ctx, name, metav1.GetOptions{})
		if start.Add(timeout).Before(time.Now()) {
			return fmt.Errorf("timeout exceeded waiting for %s is %s, error: %v", name, pod.Status.Phase, err)
		}

		if pod == nil || pod.Status.Phase == v1.PodPending {
			time.Sleep(5 * time.Second)
			continue
		}
		if pod.Status.Phase == v1.PodFailed {
			return nil
		}

		for _, phase := range phases {
			if pod.Status.Phase == phase {
				return nil
			}
		}
	}
}

func GetPodLogs(ctx context.Context, podName, container string) (string, error) {
	podLogOptions := v1.PodLogOptions{}
	if container != "" {
		podLogOptions.Container = container
	}

	kubeclient, err := ctx.Kubernetes()
	if err != nil {
		return "", fmt.Errorf("failed to get kubernetes client: %w", err)
	}

	req := kubeclient.CoreV1().Pods(ctx.GetNamespace()).GetLogs(podName, &podLogOptions)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func DeletePod(ctx context.Context, name string) error {
	kubeclient, err := ctx.Kubernetes()
	if err != nil {
		return fmt.Errorf("failed to get kubernetes client: %w", err)
	}

	return kubeclient.CoreV1().Pods(ctx.GetNamespace()).Delete(ctx, name, metav1.DeleteOptions{})
}
