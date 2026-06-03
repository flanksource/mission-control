package machinery

import (
	"context"
	"sync/atomic"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/plugin"
)

type fakeRuntime struct {
	stopped atomic.Bool
}

func (f *fakeRuntime) Invoke(context.Context, *plugin.InvokeRequest) (*plugin.InvokeResponse, error) {
	return nil, nil
}
func (f *fakeRuntime) UIPort() uint32 { return 0 }
func (f *fakeRuntime) Stop()          { f.stopped.Store(true) }

var _ = ginkgo.Describe("StopAll", func() {
	ginkgo.It("stops every running plugin and clears their runtimes", func() {
		reg := plugin.DefaultRegistry
		id1, id2 := uuid.New(), uuid.New()

		_, err := reg.Upsert(id1, "default", "p1", v1.PluginSpec{})
		Expect(err).ToNot(HaveOccurred())
		_, err = reg.Upsert(id2, "default", "p2", v1.PluginSpec{})
		Expect(err).ToNot(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			reg.Remove(id1)
			reg.Remove(id2)
		})

		r1, r2 := &fakeRuntime{}, &fakeRuntime{}
		Expect(reg.SetRuntime(id1, r1)).To(Succeed())
		Expect(reg.SetRuntime(id2, r2)).To(Succeed())

		StopAll(dutyContext.NewContext(context.Background()))

		Expect(r1.stopped.Load()).To(BeTrue())
		Expect(r2.stopped.Load()).To(BeTrue())
		Expect(reg.Get(id1).Runtime).To(BeNil())
		Expect(reg.Get(id2).Runtime).To(BeNil())
	})

	ginkgo.It("skips plugins that are not running", func() {
		reg := plugin.DefaultRegistry
		id := uuid.New()
		_, err := reg.Upsert(id, "default", "idle", v1.PluginSpec{})
		Expect(err).ToNot(HaveOccurred())
		ginkgo.DeferCleanup(func() { reg.Remove(id) })

		Expect(func() {
			StopAll(dutyContext.NewContext(context.Background()))
		}).ToNot(Panic())
	})
})
