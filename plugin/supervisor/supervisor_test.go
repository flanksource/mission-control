package supervisor

import (
	gocontext "context"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	dutyContext "github.com/flanksource/duty/context"
)

func newTestContext() (dutyContext.Context, gocontext.CancelFunc) {
	base, cancel := gocontext.WithCancel(gocontext.Background())
	return dutyContext.NewContext(base), cancel
}

var _ = ginkgo.Describe("shouldTriggerRestart", func() {
	target := "/plugins/foo"

	cases := []struct {
		name   string
		event  fsnotify.Event
		expect bool
	}{
		{"write to target", fsnotify.Event{Name: target, Op: fsnotify.Write}, true},
		{"create target", fsnotify.Event{Name: target, Op: fsnotify.Create}, true},
		{"rename target", fsnotify.Event{Name: target, Op: fsnotify.Rename}, true},
		{"write+chmod target", fsnotify.Event{Name: target, Op: fsnotify.Write | fsnotify.Chmod}, true},
		{"chmod-only target", fsnotify.Event{Name: target, Op: fsnotify.Chmod}, false},
		{"remove target", fsnotify.Event{Name: target, Op: fsnotify.Remove}, false},
		{"sibling write", fsnotify.Event{Name: "/plugins/bar", Op: fsnotify.Write}, false},
		{"unclean path matches", fsnotify.Event{Name: "/plugins/./foo", Op: fsnotify.Write}, true},
	}

	for _, tt := range cases {
		ginkgo.It(tt.name, func() {
			Expect(shouldTriggerRestart(tt.event, target)).To(Equal(tt.expect))
		})
	}
})

var _ = ginkgo.Describe("watchBinary", func() {
	var (
		dir      string
		binPath  string
		sup      *Supervisor
		restarts atomic.Int32
		ctx      dutyContext.Context
		cancel   func()
	)

	ginkgo.BeforeEach(func() {
		dir = ginkgo.GinkgoT().TempDir()
		binPath = filepath.Join(dir, "myplugin")
		Expect(os.WriteFile(binPath, []byte("v1"), 0o755)).To(Succeed())

		restarts.Store(0)
		ctx, cancel = newTestContext()

		sup = &Supervisor{
			Name:       "myplugin",
			BinaryPath: binPath,
			Debounce:   100 * time.Millisecond,
			restartFn: func(_ dutyContext.Context) error {
				restarts.Add(1)
				return nil
			},
		}

		go sup.watchBinary(ctx)
		// Give fsnotify a beat to attach the watch before we start mutating.
		time.Sleep(50 * time.Millisecond)
	})

	ginkgo.AfterEach(func() {
		sup.mu.Lock()
		sup.stopped = true
		sup.mu.Unlock()
		cancel()
	})

	ginkgo.It("restarts once when the binary is rewritten", func() {
		// Atomic replace, mimicking `go build`'s tmp+rename.
		tmp := binPath + ".tmp"
		Expect(os.WriteFile(tmp, []byte("v2"), 0o755)).To(Succeed())
		Expect(os.Rename(tmp, binPath)).To(Succeed())

		Eventually(restarts.Load, "1s", "20ms").Should(BeNumerically("==", 1))
	})

	ginkgo.It("debounces a burst of writes into a single restart", func() {
		for i := range 5 {
			Expect(os.WriteFile(binPath, []byte{byte('a' + i)}, 0o755)).To(Succeed())
			time.Sleep(10 * time.Millisecond)
		}

		Eventually(restarts.Load, "1s", "20ms").Should(BeNumerically("==", 1))
		Consistently(restarts.Load, "200ms", "20ms").Should(BeNumerically("==", 1))
	})

	ginkgo.It("ignores writes to sibling files in the same directory", func() {
		other := filepath.Join(dir, "otherplugin")
		Expect(os.WriteFile(other, []byte("hi"), 0o755)).To(Succeed())

		Consistently(restarts.Load, "300ms", "20ms").Should(BeNumerically("==", 0))
	})
})
