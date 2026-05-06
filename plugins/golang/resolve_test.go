package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = ginkgo.Describe("pod resolution metadata", func() {
	ginkgo.It("captures declared container ports by container", func() {
		pod := corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
			{Name: "api", Ports: []corev1.ContainerPort{{ContainerPort: 8080}, {ContainerPort: 6060}}},
			{Name: "sidecar", Ports: []corev1.ContainerPort{{ContainerPort: 9090}}},
		}}}

		Expect(containerPorts(pod)).To(Equal(map[string][]int{
			"api":     {8080, 6060},
			"sidecar": {9090},
		}))
	})
})
