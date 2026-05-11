package main

import (
	"context"
	"fmt"
	"strings"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/sdk"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

type kubeClient struct {
	dyn    dynamic.Interface
	mapper meta.RESTMapper
}

func newKubeClient(ctx context.Context, host sdk.HostClient, configItemID string) (*kubeClient, error) {
	cfg, err := buildRestConfig(ctx, host, configItemID)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic kubernetes client: %w", err)
	}
	disco, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes discovery client: %w", err)
	}
	return &kubeClient{dyn: dyn, mapper: restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(disco))}, nil
}

func buildRestConfig(ctx context.Context, host sdk.HostClient, configItemID string) (*rest.Config, error) {
	if host != nil {
		conn, err := host.GetConnection(ctx, "kubernetes", configItemID)
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

func (c *kubeClient) getObjectForConfig(ctx context.Context, item *pluginpb.ConfigItem) (*unstructured.Unstructured, ObjectRef, error) {
	kind := kubeKind(item.Type)
	if kind == "" {
		return nil, ObjectRef{}, fmt.Errorf("config item %s is not a Kubernetes object", item.Id)
	}
	name := item.Name
	if name == "" {
		name = item.Tags["name"]
	}
	if name == "" {
		return nil, ObjectRef{}, fmt.Errorf("config item %s has no Kubernetes name", item.Id)
	}
	ns := item.Tags["namespace"]
	obj, ref, err := c.getByKind(ctx, kind, ns, name)
	if err != nil {
		return nil, ObjectRef{}, err
	}
	return obj, ref, nil
}

func kubeKind(t string) string {
	if t == "" {
		return ""
	}
	parts := strings.Split(t, "::")
	return parts[len(parts)-1]
}

func (c *kubeClient) getByKind(ctx context.Context, kind, namespace, name string) (*unstructured.Unstructured, ObjectRef, error) {
	m, err := c.mappingForKind(kind)
	if err != nil {
		return nil, ObjectRef{}, err
	}
	var obj *unstructured.Unstructured
	if m.Scope.Name() == meta.RESTScopeNameNamespace {
		if namespace == "" {
			namespace = "default"
		}
		obj, err = c.dyn.Resource(m.Resource).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	} else {
		obj, err = c.dyn.Resource(m.Resource).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return nil, ObjectRef{}, fmt.Errorf("get %s/%s: %w", kind, name, err)
	}
	return obj, refFor(obj), nil
}

func (c *kubeClient) mappingForKind(kind string) (*meta.RESTMapping, error) {
	mapping, err := c.mapper.RESTMapping(schema.GroupKind{Kind: kind})
	if err == nil {
		return mapping, nil
	}
	if !meta.IsNoMatchError(err) {
		return nil, err
	}
	known := map[string]schema.GroupVersionKind{
		"Pod":         {Group: "", Version: "v1", Kind: "Pod"},
		"Service":     {Group: "", Version: "v1", Kind: "Service"},
		"ConfigMap":   {Group: "", Version: "v1", Kind: "ConfigMap"},
		"Secret":      {Group: "", Version: "v1", Kind: "Secret"},
		"Deployment":  {Group: "apps", Version: "v1", Kind: "Deployment"},
		"ReplicaSet":  {Group: "apps", Version: "v1", Kind: "ReplicaSet"},
		"StatefulSet": {Group: "apps", Version: "v1", Kind: "StatefulSet"},
		"DaemonSet":   {Group: "apps", Version: "v1", Kind: "DaemonSet"},
		"Job":         {Group: "batch", Version: "v1", Kind: "Job"},
		"CronJob":     {Group: "batch", Version: "v1", Kind: "CronJob"},
	}
	if gvk, ok := known[kind]; ok {
		return c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	}
	return nil, err
}

func (c *kubeClient) getOwner(ctx context.Context, owner metav1.OwnerReference, namespace string) (*unstructured.Unstructured, error) {
	gv, err := schema.ParseGroupVersion(owner.APIVersion)
	if err != nil {
		return nil, err
	}
	m, err := c.mapper.RESTMapping(gv.WithKind(owner.Kind).GroupKind(), gv.Version)
	if err != nil {
		return nil, err
	}
	if m.Scope.Name() == meta.RESTScopeNameNamespace {
		return c.dyn.Resource(m.Resource).Namespace(namespace).Get(ctx, owner.Name, metav1.GetOptions{})
	}
	return c.dyn.Resource(m.Resource).Get(ctx, owner.Name, metav1.GetOptions{})
}

func (c *kubeClient) getOptional(ctx context.Context, apiVersion, kind, namespace, name string) (*unstructured.Unstructured, bool, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return nil, false, err
	}
	return c.getOptionalByMapping(ctx, gv.WithKind(kind).GroupKind(), gv.Version, namespace, name)
}

func (c *kubeClient) getOptionalKind(ctx context.Context, group, kind, namespace, name string) (*unstructured.Unstructured, bool, error) {
	return c.getOptionalByMapping(ctx, schema.GroupKind{Group: group, Kind: kind}, "", namespace, name)
}

func (c *kubeClient) getOptionalByMapping(ctx context.Context, gk schema.GroupKind, version, namespace, name string) (*unstructured.Unstructured, bool, error) {
	var m *meta.RESTMapping
	var err error
	if version == "" {
		m, err = c.mapper.RESTMapping(gk)
	} else {
		m, err = c.mapper.RESTMapping(gk, version)
	}
	if err != nil {
		if meta.IsNoMatchError(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var obj *unstructured.Unstructured
	if m.Scope.Name() == meta.RESTScopeNameNamespace {
		obj, err = c.dyn.Resource(m.Resource).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	} else {
		obj, err = c.dyn.Resource(m.Resource).Get(ctx, name, metav1.GetOptions{})
	}
	if errors.IsNotFound(err) {
		return nil, false, nil
	}
	return obj, err == nil, err
}

func refFor(obj *unstructured.Unstructured) ObjectRef {
	return ObjectRef{APIVersion: obj.GetAPIVersion(), Kind: obj.GetKind(), Namespace: obj.GetNamespace(), Name: obj.GetName()}
}
