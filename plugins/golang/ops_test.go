package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("port candidates", func() {
	ginkgo.It("prefers discovered gops ports over defaults", func() {
		Expect(gopsCandidatePorts(4321, 6061, []int{6061, 7070})).To(Equal([]int{4321}))
	})

	ginkgo.It("falls back to configured and default gops ports", func() {
		Expect(gopsCandidatePorts(0, 6062, []int{6061, 6062, 7070})).To(Equal([]int{6062, 6061, 7070}))
	})

	ginkgo.It("tries declared container ports for pprof", func() {
		Expect(pprofCandidatePorts(0, 0, []int{8080, 6060, 8080})).To(Equal([]int{8080, 6060}))
	})

	ginkgo.It("uses explicit pprof port alone when supplied", func() {
		Expect(pprofCandidatePorts(7070, 6060, []int{8080})).To(Equal([]int{7070}))
	})
})
