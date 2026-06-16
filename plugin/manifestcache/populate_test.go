package manifestcache

import (
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("findBinary", func() {
	ginkgo.It("finds binaries in the versioned plugin install layout", func() {
		dir := ginkgo.GinkgoT().TempDir()
		versionDir := filepath.Join(dir, "kubernetes-logs", "v1.2.3")
		Expect(os.MkdirAll(versionDir, 0o755)).To(Succeed())
		bin := filepath.Join(versionDir, "kubernetes-logs")
		Expect(os.WriteFile(bin, []byte("binary"), 0o755)).To(Succeed())
		Expect(os.Symlink("v1.2.3", filepath.Join(dir, "kubernetes-logs", "latest"))).To(Succeed())

		got, err := findBinary(dir, "kubernetes-logs")
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(filepath.Join(dir, "kubernetes-logs", "latest", "kubernetes-logs")))
	})

	ginkgo.It("falls back to the nested legacy plugin binary", func() {
		dir := ginkgo.GinkgoT().TempDir()
		pluginDir := filepath.Join(dir, "kubernetes-logs")
		Expect(os.MkdirAll(pluginDir, 0o755)).To(Succeed())
		bin := filepath.Join(pluginDir, "kubernetes-logs")
		Expect(os.WriteFile(bin, []byte("binary"), 0o755)).To(Succeed())

		got, err := findBinary(dir, "kubernetes-logs")
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(bin))
	})
})
