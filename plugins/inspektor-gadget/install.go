package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/flanksource/incident-commander/plugin/sdk"
	igresources "github.com/inspektor-gadget/inspektor-gadget/pkg/resources"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
)

type InstallPlan struct {
	Namespace string           `json:"namespace"`
	Objects   []ManifestObject `json:"objects"`
	Manifest  string           `json:"manifest"`
}

type ManifestObject struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}

type InstallResult struct {
	Applied []ManifestObject `json:"applied"`
	Status  StatusResponse   `json:"status"`
}

func (p *InspektorGadgetPlugin) installPlan(_ context.Context, _ sdk.InvokeCtx) (any, error) {
	manifest := gadgetManifest(p.settings.GadgetTag)
	objects, err := decodeManifestObjects(manifest)
	if err != nil {
		return nil, err
	}
	return InstallPlan{Namespace: p.settings.GadgetNamespace, Objects: manifestSummaries(objects), Manifest: manifest}, nil
}

func (p *InspektorGadgetPlugin) install(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	dyn, err := p.clients.Dynamic(ctx, req.Host)
	if err != nil {
		return nil, err
	}
	disco, err := p.clients.Discovery(ctx, req.Host)
	if err != nil {
		return nil, err
	}
	objects, err := decodeManifestObjects(gadgetManifest(p.settings.GadgetTag))
	if err != nil {
		return nil, err
	}
	applied, err := applyObjects(ctx, dyn, disco, objects)
	if err != nil {
		return nil, err
	}
	cli, err := p.clients.Client(ctx, req.Host)
	if err != nil {
		return nil, err
	}
	return InstallResult{
		Applied: applied,
		Status:  inspectStatus(ctx, cli, p.settings.GadgetNamespace, p.settings.GadgetTag),
	}, nil
}

func gadgetManifest(tag string) string {
	if tag == "" {
		tag = defaultIGTag
	}
	return strings.ReplaceAll(igresources.GadgetDeployment, ":latest", ":"+tag)
}

func decodeManifestObjects(manifest string) ([]*unstructured.Unstructured, error) {
	decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	var out []*unstructured.Unstructured
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(obj); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode manifest: %w", err)
		}
		if obj.GetKind() == "" {
			continue
		}
		out = append(out, obj)
	}
	return out, nil
}

func manifestSummaries(objects []*unstructured.Unstructured) []ManifestObject {
	out := make([]ManifestObject, 0, len(objects))
	for _, obj := range objects {
		out = append(out, manifestSummary(obj))
	}
	return out
}

func manifestSummary(obj *unstructured.Unstructured) ManifestObject {
	return ManifestObject{
		APIVersion: obj.GetAPIVersion(),
		Kind:       obj.GetKind(),
		Namespace:  obj.GetNamespace(),
		Name:       obj.GetName(),
	}
}

func applyObjects(ctx context.Context, dyn dynamic.Interface, disco discovery.DiscoveryInterface, objects []*unstructured.Unstructured) ([]ManifestObject, error) {
	groupResources, err := restmapper.GetAPIGroupResources(disco)
	if err != nil {
		return nil, fmt.Errorf("discover api resources: %w", err)
	}
	mapper := restmapper.NewDiscoveryRESTMapper(groupResources)
	applied := make([]ManifestObject, 0, len(objects))
	for _, obj := range objects {
		mapping, err := mapper.RESTMapping(schema.GroupKind{Group: obj.GroupVersionKind().Group, Kind: obj.GetKind()}, obj.GroupVersionKind().Version)
		if err != nil {
			return applied, fmt.Errorf("map %s/%s: %w", obj.GetKind(), obj.GetName(), err)
		}
		body, err := toApplyJSON(obj)
		if err != nil {
			return applied, err
		}
		namespaced := dyn.Resource(mapping.Resource)
		var resource dynamic.ResourceInterface
		if mapping.Scope.Name() != meta.RESTScopeNameRoot {
			resource = namespaced.Namespace(obj.GetNamespace())
		} else {
			resource = namespaced
		}
		force := true
		if _, err := resource.Patch(ctx, obj.GetName(), types.ApplyPatchType, body, metav1.PatchOptions{
			FieldManager: "mission-control-inspektor-gadget",
			Force:        &force,
		}); err != nil {
			return applied, fmt.Errorf("apply %s/%s: %w", obj.GetKind(), obj.GetName(), err)
		}
		applied = append(applied, manifestSummary(obj))
	}
	return applied, nil
}

func toApplyJSON(obj *unstructured.Unstructured) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(obj.Object); err != nil {
		return nil, fmt.Errorf("encode %s/%s: %w", obj.GetKind(), obj.GetName(), err)
	}
	return buf.Bytes(), nil
}
