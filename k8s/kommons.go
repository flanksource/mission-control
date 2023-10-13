package k8s

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/flanksource/incident-commander/api"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WaitForPod waits for a pod to be in the specified phase, or returns an
// error if the timeout is exceeded
func WaitForPod(ctx api.Context, name string, timeout time.Duration, phases ...v1.PodPhase) error {
	pods := ctx.Kubernetes().CoreV1().Pods(ctx.Namespace())
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

func GetPodLogs(ctx api.Context, podName, container string) (string, error) {
	podLogOptions := v1.PodLogOptions{}
	if container != "" {
		podLogOptions.Container = container
	}

	req := ctx.Kubernetes().CoreV1().Pods(ctx.Namespace()).GetLogs(podName, &podLogOptions)
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

func DeletePod(ctx api.Context, name string) error {
	return ctx.Kubernetes().CoreV1().Pods(ctx.Namespace()).Delete(ctx, name, metav1.DeleteOptions{})
}
