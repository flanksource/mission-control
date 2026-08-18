package localcache

import (
	"context"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("findBinary", func() {
	ginkgo.It("finds binaries in the versioned plugin install layout", func() {
		dir := ginkgo.GinkgoT().TempDir()
		versionDir := filepath.Join(dir, "kubernetes-logs", "v1.2.3")
		Expect(os.MkdirAll(versionDir, 0o755)).To(Succeed())
		binary := filepath.Join(versionDir, "kubernetes-logs")
		Expect(os.WriteFile(binary, []byte("binary"), 0o755)).To(Succeed())
		Expect(os.Symlink("v1.2.3", filepath.Join(dir, "kubernetes-logs", "latest"))).To(Succeed())

		got, err := findBinary(dir, "kubernetes-logs")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(filepath.Join(dir, "kubernetes-logs", "latest", "kubernetes-logs")))
	})

	ginkgo.It("skips a non-executable candidate for a later executable layout", func() {
		dir := ginkgo.GinkgoT().TempDir()
		pluginDir := filepath.Join(dir, "kubernetes-logs")
		Expect(os.MkdirAll(pluginDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(pluginDir, "latest"), []byte("not executable"), 0o644)).To(Succeed())
		binary := filepath.Join(pluginDir, "kubernetes-logs")
		Expect(os.WriteFile(binary, []byte("binary"), 0o755)).To(Succeed())

		got, err := findBinary(dir, "kubernetes-logs")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(binary))
	})

	ginkgo.It("falls back to the nested legacy plugin binary", func() {
		dir := ginkgo.GinkgoT().TempDir()
		pluginDir := filepath.Join(dir, "kubernetes-logs")
		Expect(os.MkdirAll(pluginDir, 0o755)).To(Succeed())
		binary := filepath.Join(pluginDir, "kubernetes-logs")
		Expect(os.WriteFile(binary, []byte("binary"), 0o755)).To(Succeed())

		got, err := findBinary(dir, "kubernetes-logs")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(binary))
	})

	ginkgo.It("rejects non-executable prefixed fallback files", func() {
		dir := ginkgo.GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "kubernetes-logs-v1"), []byte("binary"), 0o644)).To(Succeed())

		_, err := findBinary(dir, "kubernetes-logs")
		Expect(err).To(MatchError(ContainSubstring(`plugin "kubernetes-logs" not found`)))
	})
})

var _ = ginkgo.Describe("dialAndCaptureManifest", func() {
	ginkgo.It("applies the configured timeout to plugin startup", func() {
		dir := ginkgo.GinkgoT().TempDir()
		binary := filepath.Join(dir, "stalled-plugin")
		Expect(os.WriteFile(binary, []byte("#!/bin/sh\nexec sleep 10\n"), 0o755)).To(Succeed())

		started := time.Now()
		_, err := dialAndCaptureManifest(context.Background(), binary, 100*time.Millisecond)

		Expect(err).To(HaveOccurred())
		Expect(time.Since(started)).To(BeNumerically("<", 2*time.Second))
	})
})
