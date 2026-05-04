package main

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/flanksource/incident-commander/plugin/sdk"
)

// clientCache memoises the kubernetes.Interface keyed by config item id, so
// repeated operations on the same catalog item don't re-parse the kubeconfig.
type clientCache struct {
	mu      sync.Mutex
	entries map[string]kubernetes.Interface
}

// For returns a kubernetes client appropriate for the given context. The
// host's GetConnection is preferred (the Plugin CRD's connection.kubernetes
// is the source of truth). When the host is unavailable (e.g. CLI smoke tests)
// or the connection didn't yield a kubeconfig, falls back to in-cluster.
func (c *clientCache) For(ctx context.Context, host sdk.HostClient) (kubernetes.Interface, error) {
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]kubernetes.Interface{}
	}
	c.mu.Unlock()

	cacheKey := "default"
	if existing, ok := c.lookup(cacheKey); ok {
		return existing, nil
	}

	cfg, err := buildRestConfig(ctx, host)
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes client: %w", err)
	}
	c.store(cacheKey, cs)
	return cs, nil
}

func (c *clientCache) lookup(k string) (kubernetes.Interface, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[k]
	return v, ok
}

func (c *clientCache) store(k string, v kubernetes.Interface) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[k] = v
}

// buildRestConfig prefers the host-resolved kubernetes connection; when it's
// not available, falls back to in-cluster (so the plugin still works as a
// sidecar in the same cluster).
func buildRestConfig(ctx context.Context, host sdk.HostClient) (*rest.Config, error) {
	if host != nil {
		conn, err := host.GetConnection(ctx, "kubernetes", "")
		if err == nil && conn != nil && conn.Properties != nil {
			if kc, ok := conn.Properties.AsMap()["kubeconfig"].(string); ok && kc != "" {
				cfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kc))
				if err == nil {
					return cfg, nil
				}
				// fall through: maybe it's a path, not contents.
				cfg2, err2 := clientcmd.BuildConfigFromFlags("", kc)
				if err2 == nil {
					return cfg2, nil
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
