package sdk

import (
	"sync/atomic"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("watchParentDeath", func() {
	ginkgo.It("invokes onDeath once the parent pid changes", func() {
		var ppid atomic.Int64
		ppid.Store(100)

		var calls atomic.Int32
		done := make(chan struct{})
		go watchParentDeath(100, time.Millisecond, func() int { return int(ppid.Load()) }, func() {
			calls.Add(1)
			close(done)
		})

		// Simulate the host dying: the plugin gets reparented to init/launchd.
		ppid.Store(1)

		Eventually(done, "1s").Should(BeClosed())
		Expect(calls.Load()).To(Equal(int32(1)))
	})

	ginkgo.It("does not fire while the parent is alive", func() {
		var calls atomic.Int32
		go watchParentDeath(100, time.Millisecond, func() int { return 100 }, func() {
			calls.Add(1)
		})

		Consistently(calls.Load, "200ms", "20ms").Should(Equal(int32(0)))
	})
})
