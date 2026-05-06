package k8s

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type ExecOptions struct {
	Namespace string
	Pod       string
	Container string
	Command   []string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
}

// ExecInPod runs a command inside a container via the Kubernetes exec API.
func ExecInPod(ctx context.Context, restCfg *rest.Config, opts ExecOptions) error {
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	req := clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(opts.Pod).
		Namespace(opts.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: opts.Container,
			Command:   opts.Command,
			Stdin:     opts.Stdin != nil,
			Stdout:    opts.Stdout != nil,
			Stderr:    opts.Stderr != nil,
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(restCfg, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
	}); err != nil {
		return fmt.Errorf("exec %v failed: %w", opts.Command, err)
	}
	return nil
}

// CopyFileToPod streams a local file into a container at remotePath using
// `sh -c 'cat > <remotePath>'`. Returns bytes written.
func CopyFileToPod(ctx context.Context, restCfg *rest.Config, namespace, pod, container, localPath, remotePath string) (int64, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", localPath, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", localPath, err)
	}

	var stderr bytes.Buffer
	cmd := []string{"sh", "-c", fmt.Sprintf("mkdir -p %q && cat > %q", parentDir(remotePath), remotePath)}

	if err := ExecInPod(ctx, restCfg, ExecOptions{
		Namespace: namespace,
		Pod:       pod,
		Container: container,
		Command:   cmd,
		Stdin:     f,
		Stdout:    io.Discard,
		Stderr:    &stderr,
	}); err != nil {
		return 0, fmt.Errorf("copy to pod: %w (stderr: %s)", err, stderr.String())
	}
	return info.Size(), nil
}

func parentDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			if i == 0 {
				return "/"
			}
			return p[:i]
		}
	}
	return "."
}
