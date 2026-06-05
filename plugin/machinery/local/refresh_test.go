// ABOUTME: Tests for the latest-version resolution helpers used by the plugin
// ABOUTME: refresh job: version detection, path math, and old-version removal.
package local

import (
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("IsLatest", func() {
	cases := []struct {
		version string
		expect  bool
	}{
		{"", true},
		{"latest", true},
		{"v1.2.3", false},
		{"1.2.3", false},
	}
	for _, tt := range cases {
		ginkgo.It(tt.version, func() {
			Expect(IsLatest(tt.version)).To(Equal(tt.expect))
		})
	}
})

var _ = ginkgo.Describe("VersionFromBinaryPath", func() {
	ginkgo.It("returns the version directory name", func() {
		Expect(VersionFromBinaryPath("/plugins/kubernetes-logs/v1.3.0/kubernetes-logs")).To(Equal("v1.3.0"))
	})

	ginkgo.It("returns empty for an empty path", func() {
		Expect(VersionFromBinaryPath("")).To(Equal(""))
	})
})

var _ = ginkgo.Describe("RemoveVersion", func() {
	var root string

	ginkgo.BeforeEach(func() {
		root = ginkgo.GinkgoT().TempDir()
		Expect(os.Setenv(EnvPluginPath, root)).To(Succeed())
		ginkgo.DeferCleanup(func() { _ = os.Unsetenv(EnvPluginPath) })
	})

	ginkgo.It("removes the version directory for a plugin", func() {
		versionDir := VersionedBinDirFor("kubernetes-logs", "v1.3.0")
		Expect(os.MkdirAll(versionDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(versionDir, "kubernetes-logs"), []byte("bin"), 0o755)).To(Succeed())

		Expect(RemoveVersion("kubernetes-logs", "", "v1.3.0")).To(Succeed())
		Expect(versionDir).ToNot(BeADirectory())
	})

	ginkgo.It("refuses to remove an empty version", func() {
		Expect(RemoveVersion("kubernetes-logs", "", "")).ToNot(Succeed())
	})

	ginkgo.It("refuses to remove the latest version", func() {
		Expect(RemoveVersion("kubernetes-logs", "", "latest")).ToNot(Succeed())
	})
})

var _ = ginkgo.Describe("pinVersion", func() {
	var root string

	stage := func(binName string, content []byte) {
		staging := VersionedBinDirFor(binName, "latest")
		Expect(os.MkdirAll(staging, 0o755)).To(Succeed())
		Expect(os.WriteFile(BinaryPathFor(binName, "latest"), content, 0o755)).To(Succeed())
	}

	ginkgo.BeforeEach(func() {
		root = ginkgo.GinkgoT().TempDir()
		Expect(os.Setenv(EnvPluginPath, root)).To(Succeed())
		ginkgo.DeferCleanup(func() { _ = os.Unsetenv(EnvPluginPath) })
	})

	ginkgo.It("copies the staged binary into its version directory", func() {
		stage("kubernetes-logs", []byte("v2-binary"))

		target, err := pinVersion("kubernetes-logs", "v2.0.0")
		Expect(err).ToNot(HaveOccurred())
		Expect(target).To(Equal(BinaryPathFor("kubernetes-logs", "v2.0.0")))

		Expect(os.ReadFile(target)).To(Equal([]byte("v2-binary")))

		info, err := os.Stat(target)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm() & 0o111).ToNot(BeZero())
	})

	ginkgo.It("leaves staging intact so the next check stays warm", func() {
		stage("kubernetes-logs", []byte("v2-binary"))

		_, err := pinVersion("kubernetes-logs", "v2.0.0")
		Expect(err).ToNot(HaveOccurred())

		Expect(BinaryPathFor("kubernetes-logs", "latest")).To(BeARegularFile())
	})

	ginkgo.It("reuses an already-pinned version without overwriting it", func() {
		versionDir := VersionedBinDirFor("kubernetes-logs", "v2.0.0")
		Expect(os.MkdirAll(versionDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(BinaryPathFor("kubernetes-logs", "v2.0.0"), []byte("already-here"), 0o755)).To(Succeed())
		stage("kubernetes-logs", []byte("newly-staged"))

		target, err := pinVersion("kubernetes-logs", "v2.0.0")
		Expect(err).ToNot(HaveOccurred())
		Expect(os.ReadFile(target)).To(Equal([]byte("already-here")))
	})
})
