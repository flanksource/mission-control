package main

import (
	"context"
	"errors"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("sessions", func() {
	ginkgo.It("keeps only the bounded event window", func() {
		gadget, ok := gadgetByID("trace_exec", defaultIGTag)
		Expect(ok).To(BeTrue())
		session, _ := newTraceSession(gadget, TraceTarget{Namespace: "default", Pod: "pod"}, nil, TraceDiagnostics{}, 2)

		session.AddEvent(TraceEvent{Raw: "one"})
		session.AddEvent(TraceEvent{Raw: "two"})
		session.AddEvent(TraceEvent{Raw: "three"})

		events := session.Events()
		Expect(events).To(HaveLen(2))
		Expect(events[0].Raw).To(Equal("two"))
		Expect(events[1].Raw).To(Equal("three"))
		Expect(session.Snapshot().EventCount).To(Equal(int64(3)))
	})

	ginkgo.It("records terminal errors", func() {
		gadget, _ := gadgetByID("trace_exec", defaultIGTag)
		session, _ := newTraceSession(gadget, TraceTarget{Namespace: "default", Pod: "pod"}, nil, TraceDiagnostics{}, 10)
		session.MarkRunning()
		session.MarkDone(errors.New("boom"))

		snapshot := session.Snapshot()
		Expect(snapshot.State).To(Equal("failed"))
		Expect(snapshot.Error).To(Equal("boom"))
		Expect(snapshot.StoppedAt).ToNot(BeNil())
	})
})

type fakeRunner struct {
	err error
}

func (f fakeRunner) Run(ctx context.Context, req TraceRunRequest, emit func(TraceEvent)) error {
	emit(TraceEvent{Raw: `{"comm":"sh"}`})
	return f.err
}
