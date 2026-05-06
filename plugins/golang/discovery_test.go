package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("gops discovery parsing", func() {
	ginkgo.It("parses pid port and command rows", func() {
		got := parseGopsDiscovery("pid=12 port=4567 cmd=/app/server --flag\npid=x port=1\npid=13 port=8901 cmd=\n")
		Expect(got).To(HaveLen(2))
		Expect(got[0].PID).To(Equal(12))
		Expect(got[0].Port).To(Equal(4567))
		Expect(got[0].Command).To(Equal("/app/server --flag"))
		Expect(got[1].PID).To(Equal(13))
		Expect(got[1].Port).To(Equal(8901))
	})

	ginkgo.It("selects a requested pid", func() {
		procs := []GopsProcess{{PID: 10, Port: 1000}, {PID: 20, Port: 2000}}
		got, ok := selectGopsProcess(procs, 20)
		Expect(ok).To(BeTrue())
		Expect(got.Port).To(Equal(2000))
	})
})
