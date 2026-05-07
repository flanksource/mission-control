package main

import (
	"context"
	"net/http"
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("ProfileViewerRegistry", func() {
	ginkgo.It("spawns a pprof viewer for a completed heap run and tears it down on RemoveSession", func() {
		profileBytes := generateHeapProfile()
		Expect(profileBytes).ToNot(BeEmpty())

		run, _ := NewProfileRun("session-A", "heap", "auto", 30)
		run.MarkDone(profileBytes, "pprof", nil)

		registry := NewProfileViewerRegistry()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		addr, err := registry.Get(ctx, run)
		Expect(err).ToNot(HaveOccurred())
		Expect(addr).To(MatchRegexp(`^127\.0\.0\.1:\d+$`))

		resp, err := http.Get("http://" + addr + "/")
		Expect(err).ToNot(HaveOccurred())
		resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		// A second Get returns the same address without spawning a new process.
		addr2, err := registry.Get(ctx, run)
		Expect(err).ToNot(HaveOccurred())
		Expect(addr2).To(Equal(addr))

		registry.RemoveSession("session-A")

		Eventually(func() error {
			c, err := http.DefaultClient.Get("http://" + addr + "/")
			if err != nil {
				return err
			}
			c.Body.Close()
			return nil
		}, "10s", "200ms").Should(HaveOccurred())
	})

	ginkgo.It("rejects trace runs because go tool pprof cannot render them", func() {
		run, _ := NewProfileRun("session-B", "trace", "auto", 30)
		run.MarkDone([]byte("ignored"), "pprof", nil)

		registry := NewProfileViewerRegistry()
		_, err := registry.Get(context.Background(), run)
		Expect(err).To(MatchError(ContainSubstring("trace profiles")))
	})

	ginkgo.It("rejects runs that have not completed", func() {
		run, _ := NewProfileRun("session-C", "heap", "auto", 30)

		registry := NewProfileViewerRegistry()
		_, err := registry.Get(context.Background(), run)
		Expect(err).To(MatchError(ContainSubstring("not completed")))
	})
})

func generateHeapProfile() []byte {
	for range 1000 {
		_ = make([]byte, 1024)
	}
	runtime.GC()
	tmp, err := os.CreateTemp("", "viewer-test-*.pprof")
	if err != nil {
		return nil
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()
	if err := pprof.Lookup("heap").WriteTo(tmp, 0); err != nil {
		return nil
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		return nil
	}
	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		return nil
	}
	return data
}
