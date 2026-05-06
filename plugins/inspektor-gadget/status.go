package main

import (
	"context"
	"strings"

	"github.com/flanksource/incident-commander/plugin/sdk"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type StatusResponse struct {
	Namespace     string   `json:"namespace"`
	Installed     bool     `json:"installed"`
	Ready         bool     `json:"ready"`
	Version       string   `json:"version,omitempty"`
	ExpectedTag   string   `json:"expectedTag"`
	DaemonSet     string   `json:"daemonSet,omitempty"`
	Desired       int32    `json:"desired,omitempty"`
	ReadyPods     int32    `json:"readyPods,omitempty"`
	AvailablePods int32    `json:"availablePods,omitempty"`
	Problems      []string `json:"problems,omitempty"`
}

func (p *InspektorGadgetPlugin) status(ctx context.Context, req sdk.InvokeCtx) (any, error) {
	cli, err := p.clients.Client(ctx, req.Host)
	if err != nil {
		return nil, err
	}
	return inspectStatus(ctx, cli, p.settings.GadgetNamespace, p.settings.GadgetTag), nil
}

func inspectStatus(ctx context.Context, cli kubernetes.Interface, namespace, expectedTag string) StatusResponse {
	out := StatusResponse{Namespace: namespace, ExpectedTag: expectedTag, DaemonSet: "gadget"}
	if _, err := cli.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			out.Problems = append(out.Problems, "Inspektor Gadget namespace is missing")
			return out
		}
		out.Problems = append(out.Problems, "namespace check failed: "+err.Error())
		return out
	}
	ds, err := cli.AppsV1().DaemonSets(namespace).Get(ctx, "gadget", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			out.Problems = append(out.Problems, "Inspektor Gadget DaemonSet is missing")
			return out
		}
		out.Problems = append(out.Problems, "DaemonSet check failed: "+err.Error())
		return out
	}
	out.Installed = true
	fillDaemonSetStatus(&out, ds)
	if out.Desired == 0 {
		out.Problems = append(out.Problems, "DaemonSet has no scheduled pods")
	} else if out.ReadyPods < out.Desired {
		out.Problems = append(out.Problems, "DaemonSet pods are not all ready")
	} else {
		out.Ready = true
	}
	if expectedTag != "" && out.Version != "" && out.Version != expectedTag {
		out.Problems = append(out.Problems, "DaemonSet image tag does not match plugin default")
	}
	return out
}

func fillDaemonSetStatus(out *StatusResponse, ds *appsv1.DaemonSet) {
	out.Desired = ds.Status.DesiredNumberScheduled
	out.ReadyPods = ds.Status.NumberReady
	out.AvailablePods = ds.Status.NumberAvailable
	for _, c := range ds.Spec.Template.Spec.Containers {
		if c.Name == "gadget" || strings.Contains(c.Image, "inspektor-gadget") {
			if idx := strings.LastIndex(c.Image, ":"); idx >= 0 {
				out.Version = c.Image[idx+1:]
			}
			return
		}
	}
}
