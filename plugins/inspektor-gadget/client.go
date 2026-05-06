package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/flanksource/incident-commander/plugin/sdk"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type clientCache struct {
	mu        sync.Mutex
	config    *rest.Config
	client    kubernetes.Interface
	dynamic   dynamic.Interface
	discovery discovery.DiscoveryInterface
}

func (c *clientCache) RESTConfig(ctx context.Context, host sdk.HostClient) (*rest.Config, error) {
	c.mu.Lock()
	if c.config != nil {
		defer c.mu.Unlock()
		return c.config, nil
	}
	c.mu.Unlock()

	cfg, err := buildRestConfig(ctx, host)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.config = cfg
	c.mu.Unlock()
	return cfg, nil
}

func (c *clientCache) Client(ctx context.Context, host sdk.HostClient) (kubernetes.Interface, error) {
	c.mu.Lock()
	if c.client != nil {
		defer c.mu.Unlock()
		return c.client, nil
	}
	c.mu.Unlock()

	cfg, err := c.RESTConfig(ctx, host)
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes client: %w", err)
	}
	c.mu.Lock()
	c.client = cs
	c.mu.Unlock()
	return cs, nil
}

func (c *clientCache) Dynamic(ctx context.Context, host sdk.HostClient) (dynamic.Interface, error) {
	c.mu.Lock()
	if c.dynamic != nil {
		defer c.mu.Unlock()
		return c.dynamic, nil
	}
	c.mu.Unlock()

	cfg, err := c.RESTConfig(ctx, host)
	if err != nil {
		return nil, err
	}
	dc, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic kubernetes client: %w", err)
	}
	c.mu.Lock()
	c.dynamic = dc
	c.mu.Unlock()
	return dc, nil
}

func (c *clientCache) Discovery(ctx context.Context, host sdk.HostClient) (discovery.DiscoveryInterface, error) {
	c.mu.Lock()
	if c.discovery != nil {
		defer c.mu.Unlock()
		return c.discovery, nil
	}
	c.mu.Unlock()

	cfg, err := c.RESTConfig(ctx, host)
	if err != nil {
		return nil, err
	}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("discovery kubernetes client: %w", err)
	}
	c.mu.Lock()
	c.discovery = dc
	c.mu.Unlock()
	return dc, nil
}

func buildRestConfig(ctx context.Context, host sdk.HostClient) (*rest.Config, error) {
	if host != nil {
		conn, err := host.GetConnection(ctx, "kubernetes", "")
		if err == nil && conn != nil && conn.Properties != nil {
			if kc, ok := conn.Properties.AsMap()["kubeconfig"].(string); ok && kc != "" {
				if cfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kc)); err == nil {
					return cfg, nil
				}
				if cfg, err := clientcmd.BuildConfigFromFlags("", kc); err == nil {
					return cfg, nil
				}
			}
		}
	}
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("no host kubernetes connection and no in-cluster config: %w", err)
	}
	return cfg, nil
}
