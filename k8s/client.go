package k8s

import (
	"fmt"
	"os"

	"github.com/flanksource/commons/files"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
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

func NewClientWithConfig(kubeConfig string) (kubernetes.Interface, error) {
	getter := func() (*clientcmdapi.Config, error) {
		clientCfg, err := clientcmd.NewClientConfigFromBytes([]byte(kubeConfig))
		if err != nil {
			return nil, err
		}

		apiCfg, err := clientCfg.RawConfig()
		if err != nil {
			return nil, err
		}

		return &apiCfg, nil
	}

	config, err := clientcmd.BuildConfigFromKubeconfigGetter("", getter)
	if err != nil {
		return nil, fmt.Errorf("failed to generate rest config: %w", err)
	}

	return kubernetes.NewForConfig(config)
}
