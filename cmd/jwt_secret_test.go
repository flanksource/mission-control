package cmd

import (
	"os"

	dutyAPI "github.com/flanksource/duty/api"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("ensureLocalJWTSecret", func() {
	var originalSecret string

	ginkgo.BeforeEach(func() {
		originalSecret = dutyAPI.DefaultConfig.Postgrest.JWTSecret
		dutyAPI.DefaultConfig.Postgrest.JWTSecret = ""
		ginkgo.DeferCleanup(func() {
			dutyAPI.DefaultConfig.Postgrest.JWTSecret = originalSecret
		})
	})

	ginkgo.It("loads an existing local secret", func() {
		wd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(os.Chdir(wd)).To(Succeed())
		})

		dir := ginkgo.GinkgoT().TempDir()
		Expect(os.Chdir(dir)).To(Succeed())

		Expect(os.WriteFile(localJWTSecretFile, []byte("existing-secret\n"), 0600)).To(Succeed())

		ensureLocalJWTSecret()

		Expect(dutyAPI.DefaultConfig.Postgrest.JWTSecret).To(Equal("existing-secret"))
	})

	ginkgo.It("generates and saves a local secret when missing", func() {
		wd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(os.Chdir(wd)).To(Succeed())
		})

		dir := ginkgo.GinkgoT().TempDir()
		Expect(os.Chdir(dir)).To(Succeed())

		ensureLocalJWTSecret()

		Expect(dutyAPI.DefaultConfig.Postgrest.JWTSecret).NotTo(BeEmpty())
		data, err := os.ReadFile(localJWTSecretFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal(dutyAPI.DefaultConfig.Postgrest.JWTSecret + "\n"))
	})

	ginkgo.It("does not override an explicit configured secret", func() {
		wd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(os.Chdir(wd)).To(Succeed())
		})

		dir := ginkgo.GinkgoT().TempDir()
		Expect(os.Chdir(dir)).To(Succeed())

		dutyAPI.DefaultConfig.Postgrest.JWTSecret = "configured-secret"

		ensureLocalJWTSecret()

		Expect(dutyAPI.DefaultConfig.Postgrest.JWTSecret).To(Equal("configured-secret"))
		_, statErr := os.Stat(localJWTSecretFile)
		Expect(os.IsNotExist(statErr)).To(BeTrue())
	})
})
