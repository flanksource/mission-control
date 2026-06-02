package sdk

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing/fstest"
	"time"

	"github.com/flanksource/incident-commander/plugin/api"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type httpTestPlugin struct {
	ops []Operation
}

func (p httpTestPlugin) Manifest() *api.PluginManifest {
	return &api.PluginManifest{Name: "http-test", Version: "1.0.0"}
}
func (httpTestPlugin) Configure(context.Context, map[string]any) error { return nil }
func (p httpTestPlugin) Operations() []Operation                       { return p.ops }

var _ = ginkgo.Describe("HTTP server routing", func() {
	ginkgo.It("serves static UI only from the reserved UI mount", func() {
		plugin := httpTestPlugin{}
		port, server := startHTTPServer(&serveOptions{staticAssets: fstest.MapFS{
			"assets/app.js": &fstest.MapFile{Data: []byte("static app")},
		}}, newPluginServer(plugin, 0))
		defer server.Close()

		body, status := httpGet(fmt.Sprintf("http://127.0.0.1:%d/__mc/ui/assets/app.js", port))
		Expect(status).To(Equal(http.StatusOK))
		Expect(body).To(Equal("static app"))

		_, status = httpGet(fmt.Sprintf("http://127.0.0.1:%d/assets/app.js", port))
		Expect(status).To(Equal(http.StatusNotFound))
	})

	ginkgo.It("serves only declared HTTP operation bindings under /__mc/operations", func() {
		plugin := httpTestPlugin{ops: []Operation{{
			Def: &api.OperationDef{
				Name: "logs",
				Http: []*api.HTTPBinding{{Method: http.MethodGet}},
			},
			HTTPHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(OperationFromContext(r.Context())).To(Equal("logs"))
				_, _ = w.Write([]byte("operation"))
			}),
		}}}
		port, server := startHTTPServer(&serveOptions{}, newPluginServer(plugin, 0))
		defer server.Close()

		body, status := httpGet(fmt.Sprintf("http://127.0.0.1:%d/__mc/operations/logs", port))
		Expect(status).To(Equal(http.StatusOK))
		Expect(body).To(Equal("operation"))

		_, status = httpGet(fmt.Sprintf("http://127.0.0.1:%d/__mc/operations/logs/extra", port))
		Expect(status).To(Equal(http.StatusNotFound))
	})
})

func httpGet(url string) (string, int) {
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Get(url)
	Expect(err).NotTo(HaveOccurred())
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	Expect(err).NotTo(HaveOccurred())
	return string(body), res.StatusCode
}
