package sdk

import (
	"context"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
)

var _ = ginkgo.Describe("FormatVersion", func() {
	ginkgo.It("includes version and build date", func() {
		got := FormatVersion("1.2.3", "2026-05-03 12:00:00")
		Expect(got).To(Equal("1.2.3 built 2026-05-03 12:00:00"))
	})

	ginkgo.It("omits the build date when not provided", func() {
		got := FormatVersion("1.2.3", "")
		Expect(got).To(Equal("1.2.3"))
	})
})

type stubPlugin struct {
	manifest *pluginpb.PluginManifest
}

func (s stubPlugin) Manifest() *pluginpb.PluginManifest            { return s.manifest }
func (stubPlugin) Configure(context.Context, map[string]any) error { return nil }
func (stubPlugin) Operations() []Operation                         { return nil }

var _ = ginkgo.Describe("RegisterPlugin version guard", func() {
	ginkgo.It("rejects an empty Version", func() {
		srv := newPluginServer(stubPlugin{
			manifest: &pluginpb.PluginManifest{Name: "demo", Version: ""},
		})
		_, err := srv.RegisterPlugin(context.Background(), &pluginpb.RegisterRequest{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Manifest().Version is required"))
	})

	ginkgo.It("rejects an empty Name", func() {
		srv := newPluginServer(stubPlugin{
			manifest: &pluginpb.PluginManifest{Name: "", Version: "1.0.0"},
		})
		_, err := srv.RegisterPlugin(context.Background(), &pluginpb.RegisterRequest{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Manifest().Name is required"))
	})

	ginkgo.It("accepts a populated manifest", func() {
		srv := newPluginServer(stubPlugin{
			manifest: &pluginpb.PluginManifest{Name: "demo", Version: "1.0.0"},
		})
		got, err := srv.RegisterPlugin(context.Background(), &pluginpb.RegisterRequest{})
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Name).To(Equal("demo"))
		Expect(got.Version).To(Equal("1.0.0"))
	})
})
