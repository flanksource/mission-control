// ABOUTME: Tests the latest-plugin refresh orchestration: version-change
// ABOUTME: detection and the stop/start/remove swap sequence, using fake ops.
package machinery

import (
	"context"
	"fmt"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/plugin"
)

var _ = ginkgo.Describe("RefreshLatestPlugins", func() {
	var (
		ctx    dutyContext.Context
		reg    *plugin.Registry
		id     uuid.UUID
		events []string
	)

	ginkgo.BeforeEach(func() {
		ctx = dutyContext.NewContext(context.Background())
		reg = plugin.DefaultRegistry
		id = uuid.New()
		events = nil
	})

	register := func(version, installedPath string) {
		_, err := reg.Upsert(id, "default", "foo", v1.PluginSpec{Source: "foo", Version: version})
		Expect(err).ToNot(HaveOccurred())
		if installedPath != "" {
			Expect(reg.SetInstalledPath(id, installedPath)).To(Succeed())
		}
		ginkgo.DeferCleanup(func() { reg.Remove(id) })
	}

	opsWith := func(resolvedVersion string, resolveErr error) refreshOps {
		return refreshOps{
			resolveLatest: func(_ dutyContext.Context, name, _ string) (string, string, error) {
				events = append(events, "resolve:"+name)
				if resolveErr != nil {
					return "", "", resolveErr
				}
				return fmt.Sprintf("/plugins/foo/%s/foo", resolvedVersion), resolvedVersion, nil
			},
			stop: func(pid uuid.UUID) error {
				events = append(events, "stop:"+pid.String())
				return nil
			},
			start: func(_ dutyContext.Context, pid uuid.UUID) error {
				events = append(events, "start:"+pid.String())
				return nil
			},
			removeVersion: func(name, _, version string) error {
				events = append(events, "remove:"+name+":"+version)
				return nil
			},
		}
	}

	ginkgo.It("stops, restarts and removes the old version when a newer one is resolved", func() {
		register("latest", "/plugins/foo/v1.0.0/foo")

		Expect(refreshLatestPlugins(ctx, opsWith("v2.0.0", nil))).To(Succeed())

		Expect(events).To(Equal([]string{
			"resolve:foo",
			"stop:" + id.String(),
			"start:" + id.String(),
			"remove:foo:v1.0.0",
		}))
	})

	ginkgo.It("does nothing when already at the latest version", func() {
		register("latest", "/plugins/foo/v1.0.0/foo")

		Expect(refreshLatestPlugins(ctx, opsWith("v1.0.0", nil))).To(Succeed())

		Expect(events).To(Equal([]string{"resolve:foo"}))
	})

	ginkgo.It("skips plugins pinned to a concrete version", func() {
		register("v1.2.3", "/plugins/foo/v1.2.3/foo")

		Expect(refreshLatestPlugins(ctx, opsWith("v2.0.0", nil))).To(Succeed())

		Expect(events).To(BeEmpty())
	})

	ginkgo.It("reports the error and does not restart when resolution fails", func() {
		register("latest", "/plugins/foo/v1.0.0/foo")

		err := refreshLatestPlugins(ctx, opsWith("", fmt.Errorf("network down")))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("network down"))

		Expect(events).To(Equal([]string{"resolve:foo"}))
	})
})
