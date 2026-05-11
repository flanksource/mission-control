package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func runDetectors(ctx context.Context, cli *kubeClient, obj *unstructured.Unstructured, params ExplainParams, includeManagedFields bool, report *ExplainReport) {
	wanted := detectorSet(params.Detectors)
	candidates := []*unstructured.Unstructured{obj}
	if wanted("runtime") {
		candidates = append(candidates, detectRuntime(ctx, cli, obj, params.MaxOwnerDepth, report)...)
	}
	for _, candidate := range candidates {
		if wanted("argo") {
			detectArgo(ctx, cli, candidate, report)
		}
		if wanted("flux") {
			detectFlux(ctx, cli, candidate, report)
		}
		if wanted("helm") {
			detectHelm(candidate, report)
		}
	}
	if wanted("kubectl") || includeManagedFields {
		detectWriters(obj, includeManagedFields, report)
	}
}

func detectorSet(names []string) func(string) bool {
	if len(names) == 0 {
		return func(string) bool { return true }
	}
	m := map[string]struct{}{}
	for _, n := range names {
		m[strings.ToLower(n)] = struct{}{}
	}
	return func(name string) bool {
		_, ok := m[name]
		return ok
	}
}

func detectRuntime(ctx context.Context, cli *kubeClient, obj *unstructured.Unstructured, maxDepth int, report *ExplainReport) []*unstructured.Unstructured {
	current := obj
	var owners []*unstructured.Unstructured
	for depth := 0; depth < maxDepth; depth++ {
		refs := current.GetOwnerReferences()
		if len(refs) == 0 {
			break
		}
		owner := controllerOwner(refs)
		if owner == nil {
			owner = &refs[0]
		}
		next, err := cli.getOwner(ctx, *owner, current.GetNamespace())
		if err != nil {
			report.addEvidence("runtime", "ownerReference", "", owner.Kind+"/"+owner.Name, err.Error(), 40)
			break
		}
		owners = append(owners, next)
		report.Runtime.OwnerChain = append(report.Runtime.OwnerChain, refFor(next))
		report.addEvidence("runtime", "ownerReference", "", owner.Kind+"/"+owner.Name, "object has owner reference", 90)
		current = next
	}
	return owners
}

func controllerOwner(refs []metav1.OwnerReference) *metav1.OwnerReference {
	for i := range refs {
		if refs[i].Controller != nil && *refs[i].Controller {
			return &refs[i]
		}
	}
	return nil
}

func detectArgo(ctx context.Context, cli *kubeClient, obj *unstructured.Unstructured, report *ExplainReport) {
	ann := obj.GetAnnotations()
	appName := ""
	confidence := 0
	if v := ann["argocd.argoproj.io/tracking-id"]; v != "" {
		appName = strings.Split(v, ":")[0]
		confidence = 95
		report.addEvidence("argo", "annotation", "argocd.argoproj.io/tracking-id", v, "Argo tracking annotation", confidence)
	}
	if appName == "" {
		if v := ann["argocd.argoproj.io/instance"]; v != "" {
			appName = v
			confidence = 85
			report.addEvidence("argo", "annotation", "argocd.argoproj.io/instance", v, "Argo instance annotation", confidence)
		}
	}
	app, found := findArgoApplication(ctx, cli, appName, obj)
	if appName == "" && !found {
		return
	}
	if found && appName == "" {
		appName = app.GetName()
		confidence = 90
		report.addEvidence("argo", "application.status.resources", "", appName, "Argo Application lists this object in status.resources", confidence)
	}
	ctrl := Controller{Type: "argo", Kind: "Application", Name: appName, Confidence: confidence}
	if found {
		ctrl.Namespace = app.GetNamespace()
		ctrl.SyncStatus, _, _ = unstructured.NestedString(app.Object, "status", "sync", "status")
		ctrl.Health, _, _ = unstructured.NestedString(app.Object, "status", "health", "status")
		revision, _, _ := unstructured.NestedString(app.Object, "status", "sync", "revision")
		appendArgoSources(app, revision, report)
	}
	report.Controllers = append(report.Controllers, ctrl)
}

func findArgoApplication(ctx context.Context, cli *kubeClient, name string, target *unstructured.Unstructured) (*unstructured.Unstructured, bool) {
	m, err := cli.mapper.RESTMapping(schemaGK("argoproj.io", "Application"))
	if err != nil || meta.IsNoMatchError(err) {
		return nil, false
	}
	var list *unstructured.UnstructuredList
	if m.Scope.Name() == meta.RESTScopeNameNamespace {
		list, err = cli.dyn.Resource(m.Resource).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	} else {
		list, err = cli.dyn.Resource(m.Resource).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, false
	}
	for i := range list.Items {
		app := &list.Items[i]
		if (name != "" && app.GetName() == name) || argoApplicationHasResource(app, target) {
			return app, true
		}
	}
	return nil, false
}

func argoApplicationHasResource(app, target *unstructured.Unstructured) bool {
	resources, ok, _ := unstructured.NestedSlice(app.Object, "status", "resources")
	if !ok {
		return false
	}
	for _, r := range resources {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if fmt.Sprint(m["kind"]) == target.GetKind() && fmt.Sprint(m["name"]) == target.GetName() && fmt.Sprint(m["namespace"]) == target.GetNamespace() {
			return true
		}
	}
	return false
}

func appendArgoSources(app *unstructured.Unstructured, revision string, report *ExplainReport) {
	if source, ok, _ := unstructured.NestedMap(app.Object, "spec", "source"); ok {
		report.Sources = append(report.Sources, sourceFromArgo(source, revision))
	}
	if sources, ok, _ := unstructured.NestedSlice(app.Object, "spec", "sources"); ok {
		for _, s := range sources {
			if m, ok := s.(map[string]any); ok {
				report.Sources = append(report.Sources, sourceFromArgo(m, revision))
			}
		}
	}
	if len(report.Sources) == 0 && revision != "" {
		report.Sources = append(report.Sources, Source{Type: "git", Revision: revision})
	}
}

func sourceFromArgo(m map[string]any, revision string) Source {
	if revision == "" {
		revision = stringValue(m["targetRevision"])
	}
	return Source{Type: "git", URL: stringValue(m["repoURL"]), Path: stringValue(m["path"]), Revision: revision}
}

func detectFlux(ctx context.Context, cli *kubeClient, obj *unstructured.Unstructured, report *ExplainReport) {
	ann := obj.GetAnnotations()
	matched := false
	if name := ann["kustomize.toolkit.fluxcd.io/name"]; name != "" {
		ns := ann["kustomize.toolkit.fluxcd.io/namespace"]
		if ns == "" {
			ns = obj.GetNamespace()
		}
		detectFluxObject(ctx, cli, "kustomize.toolkit.fluxcd.io/v1", "Kustomization", ns, name, 95, report)
		report.addEvidence("flux", "annotation", "kustomize.toolkit.fluxcd.io/name", name, "Flux Kustomization annotation", 95)
		matched = true
	}
	if name := ann["helm.toolkit.fluxcd.io/name"]; name != "" {
		ns := ann["helm.toolkit.fluxcd.io/namespace"]
		if ns == "" {
			ns = obj.GetNamespace()
		}
		detectFluxObject(ctx, cli, "helm.toolkit.fluxcd.io/v2", "HelmRelease", ns, name, 95, report)
		report.addEvidence("flux", "annotation", "helm.toolkit.fluxcd.io/name", name, "Flux HelmRelease annotation", 95)
		matched = true
	}
	if !matched {
		detectFluxInventory(ctx, cli, obj, report)
	}
}

func detectFluxInventory(ctx context.Context, cli *kubeClient, target *unstructured.Unstructured, report *ExplainReport) {
	m, err := cli.mapper.RESTMapping(schemaGK("kustomize.toolkit.fluxcd.io", "Kustomization"))
	if err != nil || meta.IsNoMatchError(err) {
		return
	}
	var list *unstructured.UnstructuredList
	if m.Scope.Name() == meta.RESTScopeNameNamespace {
		list, err = cli.dyn.Resource(m.Resource).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	} else {
		list, err = cli.dyn.Resource(m.Resource).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return
	}
	for i := range list.Items {
		ks := &list.Items[i]
		if fluxInventoryHasObject(ks, target) {
			appendFluxKustomization(ks, 90, report)
			report.addEvidence("flux", "kustomization.status.inventory", "", ks.GetNamespace()+"/"+ks.GetName(), "Flux Kustomization inventory contains this object", 90)
			return
		}
	}
}

func fluxInventoryHasObject(ks, target *unstructured.Unstructured) bool {
	entries, ok, _ := unstructured.NestedSlice(ks.Object, "status", "inventory", "entries")
	if !ok {
		return false
	}
	ids := fluxInventoryIDs(target)
	for _, e := range entries {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		id := fmt.Sprint(m["id"])
		for _, candidate := range ids {
			if id == candidate {
				return true
			}
		}
		if strings.Contains(id, "_"+target.GetName()+"_") && strings.Contains(id, "_"+target.GetKind()+"_") {
			return true
		}
	}
	return false
}

func fluxInventoryIDs(target *unstructured.Unstructured) []string {
	gv, err := schema.ParseGroupVersion(target.GetAPIVersion())
	if err != nil {
		return nil
	}
	group := gv.Group
	return []string{
		fmt.Sprintf("%s_%s_%s_%s_%s", target.GetNamespace(), target.GetName(), group, target.GetKind(), gv.Version),
		fmt.Sprintf("%s_%s_%s_%s", target.GetNamespace(), target.GetName(), group, target.GetKind()),
		fmt.Sprintf("%s_%s_%s_%s", target.GetNamespace(), target.GetName(), target.GetKind(), gv.Version),
		fmt.Sprintf("%s_%s__%s", target.GetNamespace(), target.GetName(), target.GetKind()),
	}
}

func detectFluxObject(ctx context.Context, cli *kubeClient, apiVersion, kind, namespace, name string, confidence int, report *ExplainReport) {
	obj, found, err := cli.getOptional(ctx, apiVersion, kind, namespace, name)
	if !found && err == nil {
		if gv, parseErr := schema.ParseGroupVersion(apiVersion); parseErr == nil {
			obj, found, err = cli.getOptionalKind(ctx, gv.Group, kind, namespace, name)
		}
	}
	if err == nil && found {
		appendFluxKustomization(obj, confidence, report)
		return
	}
	report.Controllers = append(report.Controllers, Controller{Type: "flux", Kind: kind, Namespace: namespace, Name: name, Confidence: confidence})
}

func appendFluxKustomization(obj *unstructured.Unstructured, confidence int, report *ExplainReport) {
	ctrl := Controller{Type: "flux", Kind: obj.GetKind(), Namespace: obj.GetNamespace(), Name: obj.GetName(), Confidence: confidence}
	if conditions, ok, _ := unstructured.NestedSlice(obj.Object, "status", "conditions"); ok {
		ctrl.Ready = readyCondition(conditions)
	}
	if rev, ok, _ := unstructured.NestedString(obj.Object, "status", "lastAppliedRevision"); ok {
		report.Sources = append(report.Sources, Source{Type: "git", Revision: rev})
	}
	if ref, ok, _ := unstructured.NestedMap(obj.Object, "spec", "sourceRef"); ok {
		report.Sources = append(report.Sources, Source{Type: "flux", Name: fmt.Sprintf("%s/%s", ref["kind"], ref["name"])})
	}
	report.Controllers = append(report.Controllers, ctrl)
}

func readyCondition(conditions []any) string {
	for _, c := range conditions {
		m, ok := c.(map[string]any)
		if ok && m["type"] == "Ready" {
			return fmt.Sprint(m["status"])
		}
	}
	return ""
}

func detectHelm(obj *unstructured.Unstructured, report *ExplainReport) {
	ann := obj.GetAnnotations()
	labels := obj.GetLabels()
	release := ann["meta.helm.sh/release-name"]
	if release == "" && labels["app.kubernetes.io/managed-by"] != "Helm" {
		return
	}
	ns := ann["meta.helm.sh/release-namespace"]
	if ns == "" {
		ns = obj.GetNamespace()
	}
	report.Renderers = append(report.Renderers, Renderer{Type: "helm", Release: release, Namespace: ns, Chart: labels["helm.sh/chart"]})
	if release != "" {
		report.Controllers = append(report.Controllers, Controller{Type: "helm", Kind: "Release", Namespace: ns, Name: release, Confidence: 80})
		report.addEvidence("helm", "annotation", "meta.helm.sh/release-name", release, "Helm release annotation", 80)
	}
}

func detectWriters(obj *unstructured.Unstructured, include bool, report *ExplainReport) {
	fields := obj.GetManagedFields()
	writers := make([]Writer, 0, len(fields))
	for _, f := range fields {
		w := Writer{Manager: f.Manager, Operation: string(f.Operation), APIVersion: f.APIVersion}
		if f.Time != nil {
			t := f.Time.Time
			w.LastSeen = &t
		}
		writers = append(writers, w)
		if strings.Contains(strings.ToLower(f.Manager), "kubectl") {
			report.addEvidence("kubectl", "managedFields", "manager", f.Manager, "kubectl wrote fields on this object", 45)
		}
	}
	sort.Slice(writers, func(i, j int) bool {
		if writers[i].LastSeen == nil {
			return false
		}
		if writers[j].LastSeen == nil {
			return true
		}
		return writers[i].LastSeen.After(*writers[j].LastSeen)
	})
	if include {
		report.Writers = writers
	}
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func schemaGK(group, kind string) schema.GroupKind {
	return schema.GroupKind{Group: group, Kind: kind}
}
