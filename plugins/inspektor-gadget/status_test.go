package main

import (
	"context"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var _ = ginkgo.Describe("status", func() {
	ginkgo.It("reports a missing namespace as not installed", func() {
		status := inspectStatus(context.Background(), fake.NewSimpleClientset(), "gadget", defaultIGTag)
		Expect(status.Installed).To(BeFalse())
		Expect(status.Ready).To(BeFalse())
		Expect(status.Problems).To(ContainElement("Inspektor Gadget namespace is missing"))
	})

	ginkgo.It("reports a ready daemonset", func() {
		cli := fake.NewSimpleClientset(
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "gadget"}},
			&appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Name: "gadget", Namespace: "gadget"},
				Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name:  "gadget",
					Image: "ghcr.io/inspektor-gadget/inspektor-gadget:" + defaultIGTag,
				}}}}},
				Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 2, NumberReady: 2, NumberAvailable: 2},
			},
		)

		status := inspectStatus(context.Background(), cli, "gadget", defaultIGTag)
		Expect(status.Installed).To(BeTrue())
		Expect(status.Ready).To(BeTrue())
		Expect(status.Version).To(Equal(defaultIGTag))
		Expect(status.Problems).To(BeEmpty())
	})
})
