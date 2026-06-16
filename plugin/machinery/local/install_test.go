package local

import (
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("plugin install paths", func() {
	var previousPluginPath string

	ginkgo.BeforeEach(func() {
		previousPluginPath = os.Getenv(EnvPluginPath)
		ginkgo.DeferCleanup(func() {
			if previousPluginPath == "" {
				_ = os.Unsetenv(EnvPluginPath)
			} else {
				_ = os.Setenv(EnvPluginPath, previousPluginPath)
			}
		})
	})

	ginkgo.It("uses a versioned binary path", func() {
		root := ginkgo.GinkgoT().TempDir()
		Expect(os.Setenv(EnvPluginPath, root)).To(Succeed())

		Expect(VersionedBinDirFor("kubernetes-logs", "v1.3.0")).To(Equal(filepath.Join(root, "kubernetes-logs", "v1.3.0")))
		Expect(BinaryPathFor("kubernetes-logs", "v1.3.0")).To(Equal(filepath.Join(root, "kubernetes-logs", "v1.3.0", "kubernetes-logs")))
	})

	ginkgo.It("uses latest as the version directory when version is empty", func() {
		root := ginkgo.GinkgoT().TempDir()
		Expect(os.Setenv(EnvPluginPath, root)).To(Succeed())

		Expect(BinaryPathFor("kubernetes-logs", "")).To(Equal(filepath.Join(root, "kubernetes-logs", "latest", "kubernetes-logs")))
	})
})
