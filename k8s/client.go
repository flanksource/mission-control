package k8s

import (
	"fmt"
	"os"

	"github.com/flanksource/commons/files"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func NewClient() (kubernetes.Interface, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.ExpandEnv("$HOME/.kube/config")
	}

	if !files.Exists(kubeconfig) {
		if config, err := rest.InClusterConfig(); err == nil {
			return kubernetes.NewForConfig(config)
		} else {
			return nil, fmt.Errorf("cannot find kubeconfig")
		}
	}

	data, err := os.ReadFile(kubeconfig)
	if err != nil {
		return nil, err
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restConfig)
}
