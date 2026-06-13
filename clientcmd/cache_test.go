package clientcmd

import (
	gocontext "context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"time"

	"github.com/flanksource/clicky/rpc"
	"github.com/flanksource/incident-commander/plugin/manifestcache"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = ginkgo.Describe("faro context cache", func() {
	var restoreCache func()

	ginkgo.BeforeEach(func() {
		dir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", dir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", dir)
		contextFlag = ""
		restoreCache = SetContextCacheDirForTest(filepath.Join(dir, "cache"))
	})

	ginkgo.AfterEach(func() {
		restoreCache()
	})

	ginkgo.It("records successful empty refreshes so faro does not refetch every run", func() {
		calls := 0
		server := pluginListServer(func() []string {
			calls++
			return nil
		})
		defer server.Close()
		storeContext("prod", server.URL)

		result, err := EnsureCurrentContextCache(gocontext.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())
		Expect(result.Refreshed).To(BeTrue())
		Expect(result.Plugins).To(BeEmpty())
		Expect(calls).To(Equal(1))
		Expect(contextLastRanPath("prod")).To(BeAnExistingFile())

		result, err = EnsureCurrentContextCache(gocontext.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Refreshed).To(BeFalse())
		Expect(calls).To(Equal(1))
	})

	ginkgo.It("clears stale plugin sidecars when rebuilding", func() {
		plugins := []string{"kubernetes-logs"}
		server := pluginListServer(func() []string { return plugins })
		defer server.Close()
		storeContext("prod", server.URL)

		result, err := RebuildCurrentContextCache(gocontext.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Plugins).To(ConsistOf("kubernetes-logs"))
		Expect(manifestcache.PathInDir(contextPluginCacheDir("prod"), "kubernetes-logs")).To(BeAnExistingFile())

		plugins = nil
		result, err = RebuildCurrentContextCache(gocontext.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Plugins).To(BeEmpty())
		Expect(manifestcache.PathInDir(contextPluginCacheDir("prod"), "kubernetes-logs")).NotTo(BeAnExistingFile())
		Expect(contextLastRanPath("prod")).To(BeAnExistingFile())
	})

	ginkgo.It("registers dynamic commands only from the current context cache", func() {
		globalRestore := manifestcache.SetDirForTest(filepath.Join(ginkgo.GinkgoT().TempDir(), "global"))
		defer globalRestore()
		Expect(manifestcache.Write(manifestcache.Entry{
			Source: manifestcache.SourceRemoteServer,
			Service: rpc.RPCService{
				Name: "wrong-server-plugin",
			},
		})).To(Succeed())

		storeContext("prod", "https://prod.example.test")
		Expect(manifestcache.WriteToDir(contextPluginCacheDir("prod"), manifestcache.Entry{
			Source: manifestcache.SourceRemoteServer,
			Service: rpc.RPCService{
				Name: "right-server-plugin",
			},
		})).To(Succeed())

		root := &cobra.Command{Use: "faro"}
		Expect(RegisterContextCachedPluginCommands(root)).To(Succeed())
		Expect(commandExists(root, "right-server-plugin")).To(BeTrue())
		Expect(commandExists(root, "wrong-server-plugin")).To(BeFalse())
	})

	ginkgo.It("detects expired cache timestamps", func() {
		Expect(writeContextLastRan("prod", time.Now().Add(-25*time.Hour))).To(Succeed())
		Expect(shouldRefreshContextCache("prod", time.Now())).To(BeTrue())
	})
})

func storeContext(name, serverURL string) {
	Expect(SaveConfig(&MCConfig{
		CurrentContext: name,
		Contexts: []MCContext{{
			Name:   name,
			Server: serverURL,
			Token:  "token",
		}},
	})).To(Succeed())
}

func pluginListServer(names func() []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer ginkgo.GinkgoRecover()

		Expect(r.Method).To(Equal(http.MethodGet))
		Expect(r.URL.Path).To(Equal("/api/plugins"))
		Expect(r.URL.Query().Get("format")).To(Equal("clicky-rpc"))

		pluginNames := names()
		items := make([]map[string]any, 0, len(pluginNames))
		for _, name := range pluginNames {
			items = append(items, map[string]any{
				"name": name,
				"service": rpc.RPCService{
					Name: name,
					Operations: []rpc.RPCOperation{{
						Name: "tail",
					}},
				},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		Expect(json.NewEncoder(w).Encode(items)).To(Succeed())
	}))
}
