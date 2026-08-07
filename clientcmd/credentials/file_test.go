package credentials

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/flanksource/incident-commander/auth/oidcclient"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func credential(refresh string) *Credential {
	return &Credential{OIDC: &oidcclient.Tokens{AccessToken: "access-" + refresh, RefreshToken: refresh}}
}

var _ = ginkgo.Describe("FileStore", func() {
	var dir string
	var store *FileStore

	ginkgo.BeforeEach(func() {
		dir = ginkgo.GinkgoT().TempDir()
		store = NewFileStore(dir)
	})

	ginkgo.It("returns nil for a context that was never stored", func() {
		Expect(store.Get("beta")).To(BeNil())
	})

	ginkgo.It("round-trips a credential", func() {
		Expect(store.Set("beta", credential("refresh-1"))).To(Succeed())
		Expect(store.Get("beta")).To(Equal(credential("refresh-1")))
	})

	ginkgo.It("isolates the caller from the stored copy", func() {
		Expect(store.Set("beta", credential("refresh-1"))).To(Succeed())

		got, err := store.Get("beta")
		Expect(err).To(Succeed())
		got.OIDC.RefreshToken = "mutated"

		Expect(store.Get("beta")).To(Equal(credential("refresh-1")))
	})

	ginkgo.It("keeps other contexts when one is replaced", func() {
		Expect(store.Set("beta", credential("refresh-beta"))).To(Succeed())
		Expect(store.Set("app", credential("refresh-app"))).To(Succeed())
		Expect(store.Set("beta", credential("refresh-beta-2"))).To(Succeed())

		Expect(store.Get("app")).To(Equal(credential("refresh-app")))
		Expect(store.Get("beta")).To(Equal(credential("refresh-beta-2")))
	})

	ginkgo.It("drops the entry when the credential is empty", func() {
		Expect(store.Set("beta", credential("refresh-1"))).To(Succeed())
		Expect(store.Set("beta", &Credential{})).To(Succeed())
		Expect(store.Get("beta")).To(BeNil())
	})

	ginkgo.It("persists the terminal re-auth marker", func() {
		Expect(store.Set("beta", &Credential{NeedsReauth: "refresh token rejected"})).To(Succeed())

		got, err := store.Get("beta")
		Expect(err).To(Succeed())
		Expect(got.NeedsReauth).To(Equal("refresh token rejected"))
		Expect(got.OIDC).To(BeNil())
	})

	ginkgo.It("deletes a context without disturbing the others", func() {
		Expect(store.Set("beta", credential("refresh-beta"))).To(Succeed())
		Expect(store.Set("app", credential("refresh-app"))).To(Succeed())

		Expect(store.Delete("beta")).To(Succeed())
		Expect(store.Get("beta")).To(BeNil())
		Expect(store.Get("app")).To(Equal(credential("refresh-app")))
	})

	ginkgo.It("deleting an absent context is not an error", func() {
		Expect(store.Delete("never-stored")).To(Succeed())
	})

	ginkgo.It("rejects an empty context name rather than storing it under one", func() {
		Expect(store.Set("", credential("refresh-1"))).ToNot(Succeed())
	})

	ginkgo.It("reports a corrupt file instead of silently starting over", func() {
		Expect(os.WriteFile(filepath.Join(dir, credentialsFile), []byte("{not json"), 0600)).To(Succeed())

		_, err := store.Get("beta")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(credentialsFile))
	})

	ginkgo.Describe("durability", func() {
		ginkgo.It("writes the file 0600", func() {
			Expect(store.Set("beta", credential("refresh-1"))).To(Succeed())

			info, err := os.Stat(filepath.Join(dir, credentialsFile))
			Expect(err).To(Succeed())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))
		})

		ginkgo.It("tightens the mode of a pre-existing world-readable file", func() {
			path := filepath.Join(dir, credentialsFile)
			Expect(os.WriteFile(path, []byte(`{"contexts":{}}`), 0644)).To(Succeed())

			Expect(store.Set("beta", credential("refresh-1"))).To(Succeed())

			info, err := os.Stat(path)
			Expect(err).To(Succeed())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))
		})

		ginkgo.It("leaves no temp file behind on a successful write", func() {
			Expect(store.Set("beta", credential("refresh-1"))).To(Succeed())

			entries, err := os.ReadDir(dir)
			Expect(err).To(Succeed())
			names := []string{}
			for _, e := range entries {
				names = append(names, e.Name())
			}
			Expect(names).To(ConsistOf(credentialsFile))
		})

		// A crash between the temp write and the rename leaves a stray temp file.
		// The previous version must still be the one readers see — that is the
		// whole point of writing to a temp file rather than over the live path.
		ginkgo.It("ignores a temp file abandoned by a crashed write", func() {
			Expect(store.Set("beta", credential("refresh-1"))).To(Succeed())

			abandoned := filepath.Join(dir, "."+credentialsFile+"-crashed.tmp")
			Expect(os.WriteFile(abandoned, []byte(`{"contexts":{"beta":{"oidc":{"refresh_token":"half`), 0600)).To(Succeed())

			Expect(store.Get("beta")).To(Equal(credential("refresh-1")))
		})

		ginkgo.It("reports an unwritable directory before anything is spent", func() {
			if os.Geteuid() == 0 {
				ginkgo.Skip("root ignores directory permissions")
			}
			Expect(os.Chmod(dir, 0500)).To(Succeed())
			ginkgo.DeferCleanup(func() { _ = os.Chmod(dir, 0700) })

			Expect(store.Writable()).ToNot(Succeed())
		})

		ginkgo.It("reports a writable directory and leaves no probe behind", func() {
			Expect(store.Writable()).To(Succeed())

			entries, err := os.ReadDir(dir)
			Expect(err).To(Succeed())
			Expect(entries).To(BeEmpty())
		})

		ginkgo.It("creates the directory 0700 when it does not exist yet", func() {
			nested := filepath.Join(dir, "missing")
			Expect(NewFileStore(nested).Writable()).To(Succeed())

			info, err := os.Stat(nested)
			Expect(err).To(Succeed())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0700)))
		})
	})

	// The read-modify-write in the old SaveConfig lost every writer but one.
	ginkgo.It("keeps every context when writers race", func() {
		const writers = 8

		var wg sync.WaitGroup
		errs := make([]error, writers)
		for i := range writers {
			wg.Add(1)
			go func() {
				defer ginkgo.GinkgoRecover()
				defer wg.Done()
				name := fmt.Sprintf("context-%d", i)
				errs[i] = WithLock(dir, func() error { return store.Set(name, credential(name)) })
			}()
		}
		wg.Wait()

		for i, err := range errs {
			Expect(err).To(Succeed(), "writer %d", i)
		}
		for i := range writers {
			name := fmt.Sprintf("context-%d", i)
			Expect(store.Get(name)).To(Equal(credential(name)), name)
		}
	})

	ginkgo.It("never writes a partial file that a concurrent reader can see", func() {
		done := make(chan struct{})
		go func() {
			defer ginkgo.GinkgoRecover()
			defer close(done)
			for i := range 50 {
				Expect(WithLock(dir, func() error {
					return store.Set("beta", credential(fmt.Sprintf("refresh-%d", i)))
				})).To(Succeed())
			}
		}()

		for {
			select {
			case <-done:
				return
			default:
			}
			data, err := os.ReadFile(filepath.Join(dir, credentialsFile))
			if os.IsNotExist(err) {
				continue
			}
			Expect(err).To(Succeed())
			Expect(json.Unmarshal(data, &fileContents{})).To(Succeed(), string(data))
		}
	})
})

var _ = ginkgo.Describe("WithLock", func() {
	ginkgo.It("serialises overlapping critical sections", func() {
		dir := ginkgo.GinkgoT().TempDir()

		var mu sync.Mutex
		inside, maxInside := 0, 0

		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer ginkgo.GinkgoRecover()
				defer wg.Done()
				Expect(WithLock(dir, func() error {
					mu.Lock()
					inside++
					maxInside = max(maxInside, inside)
					mu.Unlock()

					defer func() {
						mu.Lock()
						inside--
						mu.Unlock()
					}()
					return nil
				})).To(Succeed())
			}()
		}
		wg.Wait()

		Expect(maxInside).To(Equal(1))
	})

	ginkgo.It("propagates the error from the critical section", func() {
		err := WithLock(ginkgo.GinkgoT().TempDir(), func() error { return fmt.Errorf("boom") })
		Expect(err).To(MatchError("boom"))
	})

	ginkgo.It("releases the lock after the critical section fails", func() {
		dir := ginkgo.GinkgoT().TempDir()

		Expect(WithLock(dir, func() error { return fmt.Errorf("boom") })).To(HaveOccurred())
		Expect(WithLock(dir, func() error { return nil })).To(Succeed())
	})
})

var _ = ginkgo.Describe("Open", func() {
	ginkgo.It("defaults to the file store", func() {
		store, err := Open(ginkgo.GinkgoT().TempDir(), "")
		Expect(err).To(Succeed())
		Expect(store.Name()).To(Equal(KindFile))
	})

	ginkgo.It("refuses an unknown store rather than guessing", func() {
		_, err := Open(ginkgo.GinkgoT().TempDir(), "vault")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("vault"))
	})
})
