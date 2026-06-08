package manifestcache

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/flanksource/clicky/rpc"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Sidecar cache", func() {
	var tmpDir string
	var restore func()

	ginkgo.BeforeEach(func() {
		tmpDir = ginkgo.GinkgoT().TempDir()
		restore = SetDirForTest(tmpDir)
	})

	ginkgo.AfterEach(func() {
		restore()
	})

	ginkgo.It("returns ErrMissing when no entry exists", func() {
		_, err := Get("does-not-exist")
		Expect(errors.Is(err, ErrMissing)).To(BeTrue())
	})

	ginkgo.It("round-trips a remote-server entry", func() {
		entry := Entry{
			Source:    SourceRemoteServer,
			ServerURL: "https://example.test",
			Service: rpc.RPCService{
				Name:    "alpha",
				Version: "v1.2.3",
				Operations: []rpc.RPCOperation{
					{Name: "op-a", Description: "first op"},
					{Name: "op-b", Description: "second op"},
				},
			},
		}
		Expect(Write(entry)).To(Succeed())

		got, err := Get("alpha")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Source).To(Equal(SourceRemoteServer))
		Expect(got.ServerURL).To(Equal("https://example.test"))
		Expect(got.Service.Name).To(Equal("alpha"))
		Expect(got.Service.Operations).To(HaveLen(2))
		Expect(got.CachedAt.IsZero()).To(BeFalse())
	})

	ginkgo.It("returns ErrStale when binary checksum no longer matches", func() {
		bin := filepath.Join(tmpDir, "fake-plugin-bin")
		Expect(os.WriteFile(bin, []byte("v1-content"), 0o755)).To(Succeed())

		sum, err := SHA256File(bin)
		Expect(err).NotTo(HaveOccurred())

		Expect(Write(Entry{
			Source:         SourceLocalBinary,
			BinaryPath:     bin,
			BinaryChecksum: sum,
			Service:        rpc.RPCService{Name: "beta"},
		})).To(Succeed())

		fresh, err := Get("beta")
		Expect(err).NotTo(HaveOccurred())
		Expect(fresh.Service.Name).To(Equal("beta"))

		Expect(os.WriteFile(bin, []byte("v2-different-content"), 0o755)).To(Succeed())

		stale, err := Get("beta")
		Expect(errors.Is(err, ErrStale)).To(BeTrue())
		Expect(stale).NotTo(BeNil())
		Expect(stale.Service.Name).To(Equal("beta"))
	})

	ginkgo.It("returns ErrStale when the binary has been removed", func() {
		bin := filepath.Join(tmpDir, "removed-bin")
		Expect(os.WriteFile(bin, []byte("x"), 0o755)).To(Succeed())
		sum, err := SHA256File(bin)
		Expect(err).NotTo(HaveOccurred())

		Expect(Write(Entry{
			Source:         SourceLocalBinary,
			BinaryPath:     bin,
			BinaryChecksum: sum,
			Service:        rpc.RPCService{Name: "gamma"},
		})).To(Succeed())

		Expect(os.Remove(bin)).To(Succeed())

		_, err = Get("gamma")
		Expect(errors.Is(err, ErrStale)).To(BeTrue())
	})

	ginkgo.It("returns ErrCorrupt when the sidecar JSON is malformed", func() {
		Expect(os.MkdirAll(Dir(), 0o755)).To(Succeed())
		Expect(os.WriteFile(Path("delta"), []byte("{not json"), 0o644)).To(Succeed())

		_, err := Get("delta")
		Expect(errors.Is(err, ErrCorrupt)).To(BeTrue())
	})

	ginkgo.It("Delete removes the sidecar and is idempotent", func() {
		Expect(Write(Entry{
			Source:  SourceRemoteServer,
			Service: rpc.RPCService{Name: "epsilon"},
		})).To(Succeed())

		Expect(Path("epsilon")).To(BeAnExistingFile())
		Expect(Delete("epsilon")).To(Succeed())
		Expect(Path("epsilon")).NotTo(BeAnExistingFile())

		Expect(Delete("epsilon")).To(Succeed())
	})

	ginkgo.It("List returns every cached entry, skipping non-JSON files", func() {
		Expect(Write(Entry{Source: SourceRemoteServer, Service: rpc.RPCService{Name: "one"}})).To(Succeed())
		Expect(Write(Entry{Source: SourceRemoteServer, Service: rpc.RPCService{Name: "two"}})).To(Succeed())
		Expect(os.WriteFile(filepath.Join(Dir(), "README.md"), []byte("ignore"), 0o644)).To(Succeed())

		entries, err := List()
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(2))

		names := []string{entries[0].Service.Name, entries[1].Service.Name}
		Expect(names).To(ConsistOf("one", "two"))
	})

	ginkgo.It("Write rejects entries with an empty service name", func() {
		err := Write(Entry{Source: SourceRemoteServer})
		Expect(err).To(HaveOccurred())
	})

	ginkgo.It("Write leaves no temp file behind on a successful rename", func() {
		Expect(Write(Entry{
			Source:  SourceRemoteServer,
			Service: rpc.RPCService{Name: "zeta"},
		})).To(Succeed())

		entries, err := os.ReadDir(Dir())
		Expect(err).NotTo(HaveOccurred())
		for _, e := range entries {
			Expect(e.Name()).NotTo(HaveSuffix(".tmp"))
		}
	})
})
