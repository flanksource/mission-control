package ui

import (
	gocontext "context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("UI dev server helpers", func() {
	ginkgo.It("builds the Vite dev command with backend proxy env", func() {
		cmd := devServerCommand(gocontext.Background(), "/tmp/frontend", 4321, "http://127.0.0.1:8080")

		Expect(filepath.Base(cmd.Path)).To(Equal("pnpm"))
		Expect(cmd.Dir).To(Equal("/tmp/frontend"))
		Expect(cmd.Args).To(Equal([]string{
			"pnpm", "exec", "vite", "--host", viteHost, "--port", "4321", "--strictPort",
		}))
		Expect(cmd.SysProcAttr).NotTo(BeNil())
		Expect(cmd.SysProcAttr.Setpgid).To(BeTrue())
		Expect(cmd.Env).To(ContainElement("INCIDENT_COMMANDER_API_URL=http://127.0.0.1:8080"))
	})

	ginkgo.It("stops idempotently", func() {
		var stops atomic.Int32
		done := make(chan struct{})
		close(done)
		srv := &DevServer{
			cancel: func() {
				stops.Add(1)
			},
			wait: &processWait{done: done},
		}

		srv.Stop()
		srv.Stop()

		Expect(stops.Load()).To(Equal(int32(1)))
	})

	ginkgo.It("chooses a bindable random port", func() {
		port, err := freePort()
		Expect(err).NotTo(HaveOccurred())
		Expect(port).To(BeNumerically(">", 0))

		listener, err := net.Listen("tcp", net.JoinHostPort(viteHost, strconv.Itoa(port)))
		Expect(err).NotTo(HaveOccurred())
		_ = listener.Close()
	})

	ginkgo.It("finds ui/frontend from a child working directory", func() {
		original, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		defer os.Chdir(original) //nolint:errcheck

		tmp, err := filepath.EvalSymlinks(ginkgo.GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		frontend := filepath.Join(tmp, "ui", "frontend")
		Expect(os.MkdirAll(frontend, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(frontend, "package.json"), []byte("{}"), 0o644)).To(Succeed())
		child := filepath.Join(tmp, "cmd", "server")
		Expect(os.MkdirAll(child, 0o755)).To(Succeed())
		Expect(os.Chdir(child)).To(Succeed())

		found, err := findFrontendDir()
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(Equal(frontend))
	})
})
