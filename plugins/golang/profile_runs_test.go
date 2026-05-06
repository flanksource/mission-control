package main

import (
	"errors"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("ProfileRegistry", func() {
	ginkgo.It("tracks completed run snapshots without exposing profile bytes", func() {
		registry := NewProfileRegistry()
		run, _ := NewProfileRun("s1", "heap", "auto", 30)
		run.MarkDone([]byte("heap-profile"), "pprof", nil)
		registry.Add(run)

		runs := registry.List("s1")

		Expect(runs).To(HaveLen(1))
		Expect(runs[0].State).To(Equal("completed"))
		Expect(runs[0].Bytes).To(Equal(len("heap-profile")))
		Expect(runs[0].URL).To(Equal("profiles/s1/" + run.ID))
		Expect(runs[0].Data()).To(BeEmpty())
		Expect(run.Data()).To(Equal([]byte("heap-profile")))
	})

	ginkgo.It("records failed and stopped runs", func() {
		failed, _ := NewProfileRun("s1", "cpu", "gops", 30)
		failed.MarkDone(nil, "gops", errors.New("boom"))
		stopped, _ := NewProfileRun("s1", "trace", "pprof", 30)
		stopped.Stop()
		stopped.MarkDone([]byte("late"), "pprof", nil)

		Expect(failed.Snapshot().State).To(Equal("failed"))
		Expect(failed.Snapshot().Error).To(ContainSubstring("boom"))
		Expect(stopped.Snapshot().State).To(Equal("stopped"))
		Expect(stopped.Data()).To(BeEmpty())
	})
})
