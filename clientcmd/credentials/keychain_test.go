package credentials

import (
	"encoding/json"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These specs mutate the developer's real login keychain, so they are opt-in.
var _ = ginkgo.Describe("KeychainStore", ginkgo.Ordered, func() {
	const context = "credentials-test-context"

	var dir string
	var store *KeychainStore

	ginkgo.BeforeEach(func() {
		if os.Getenv("MC_TEST_KEYCHAIN") != "1" {
			ginkgo.Skip("set MC_TEST_KEYCHAIN=1 to run against the OS keychain")
		}
		dir = ginkgo.GinkgoT().TempDir()
		store = NewKeychainStore(dir)
		ginkgo.DeferCleanup(func() { _ = store.Delete(context) })
	})

	ginkgo.It("reports the keychain writable on a machine that has one", func() {
		Expect(store.Writable()).To(Succeed())
	})

	ginkgo.It("returns nil for a context that was never stored", func() {
		Expect(store.Get(context)).To(BeNil())
	})

	ginkgo.It("round-trips a credential across both halves", func() {
		Expect(store.Set(context, credential("refresh-1"))).To(Succeed())
		Expect(store.Get(context)).To(Equal(credential("refresh-1")))
	})

	// The whole point of the keychain backend: the long-lived secret must not
	// be readable from the filesystem.
	ginkgo.It("keeps the refresh token out of credentials.json", func() {
		Expect(store.Set(context, credential("refresh-1"))).To(Succeed())

		data, err := os.ReadFile(filepath.Join(dir, credentialsFile))
		Expect(err).To(Succeed())
		Expect(string(data)).ToNot(ContainSubstring("refresh-1"))
		Expect(string(data)).To(ContainSubstring("access-refresh-1"))
	})

	ginkgo.It("keeps a static API token out of credentials.json", func() {
		Expect(store.Set(context, &Credential{Token: "static-token"})).To(Succeed())
		Expect(store.Get(context)).To(Equal(&Credential{Token: "static-token"}))

		data, err := os.ReadFile(filepath.Join(dir, credentialsFile))
		if !os.IsNotExist(err) {
			Expect(err).To(Succeed())
			Expect(string(data)).ToNot(ContainSubstring("static-token"))
		}
	})

	ginkgo.It("persists the terminal re-auth marker in the file half", func() {
		Expect(store.Set(context, &Credential{NeedsReauth: "refresh token rejected"})).To(Succeed())

		got, err := store.Get(context)
		Expect(err).To(Succeed())
		Expect(got.NeedsReauth).To(Equal("refresh token rejected"))
	})

	ginkgo.It("clears the keychain item when the secrets are removed", func() {
		Expect(store.Set(context, credential("refresh-1"))).To(Succeed())
		Expect(store.Set(context, &Credential{NeedsReauth: "refresh token rejected"})).To(Succeed())

		got, err := store.Get(context)
		Expect(err).To(Succeed())
		Expect(got.OIDC).To(BeNil())
	})

	ginkgo.It("deletes both halves", func() {
		Expect(store.Set(context, credential("refresh-1"))).To(Succeed())
		Expect(store.Delete(context)).To(Succeed())
		Expect(store.Get(context)).To(BeNil())
	})

	ginkgo.It("deleting an absent context is not an error", func() {
		Expect(store.Delete("credentials-test-never-stored")).To(Succeed())
	})

	ginkgo.It("stores a compact item well under the Windows credential size limit", func() {
		Expect(store.Set(context, credential("refresh-1"))).To(Succeed())

		secret, err := store.readSecret(context)
		Expect(err).To(Succeed())
		raw, err := json.Marshal(secret)
		Expect(err).To(Succeed())
		Expect(len(raw)).To(BeNumerically("<", 2560))
	})
})
