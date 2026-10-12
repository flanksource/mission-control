package plugin

import (
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/plugin/api"
)

var _ = ginkgo.Describe("Registry changes", func() {
	var (
		registry *Registry
		changes  int
		id       uuid.UUID
	)

	manifest := func(ops ...string) *api.PluginManifest {
		m := &api.PluginManifest{}
		for _, op := range ops {
			m.Operations = append(m.Operations, &api.OperationDef{Name: op})
		}
		return m
	}

	ginkgo.BeforeEach(func() {
		registry = New()
		changes = 0
		registry.OnPluginsChanged(func() { changes++ })
		id = uuid.New()
		_, err := registry.Upsert(id, "default", "logs", v1.PluginSpec{Source: "test"})
		Expect(err).ToNot(HaveOccurred())
		Expect(changes).To(Equal(1), "a new plugin")
		changes = 0
	})

	ginkgo.It("lists no operations for a plugin without a manifest", func() {
		Expect(registry.Operations()).To(BeEmpty())
	})

	ginkgo.It("reports removing a plugin that never registered", func() {
		registry.Remove(id)
		Expect(changes).To(Equal(1))
	})

	ginkgo.It("reports a manifest without operations", func() {
		Expect(registry.SetManifest(id, manifest())).To(Succeed())
		Expect(registry.Operations()).To(BeEmpty())
		Expect(changes).To(Equal(1))

		Expect(registry.SetManifest(id, manifest("tail"))).To(Succeed())
		Expect(changes).To(Equal(2))

		Expect(registry.SetManifest(id, manifest())).To(Succeed())
		Expect(changes).To(Equal(3), "its operations removed")
	})

	ginkgo.It("reports when a plugin's operations are installed, upgraded or removed", func() {
		Expect(registry.SetManifest(id, manifest("tail", "exec"))).To(Succeed())
		Expect(registry.Operations()).To(Equal(map[string][]string{"logs": {"exec", "tail"}}))
		Expect(changes).To(Equal(1))

		Expect(registry.SetManifest(id, manifest("exec", "tail"))).To(Succeed())
		Expect(changes).To(Equal(1), "same operations")

		_, err := registry.Upsert(id, "default", "logs", v1.PluginSpec{Source: "test", Version: "2"})
		Expect(err).ToNot(HaveOccurred())
		Expect(changes).To(Equal(1), "a new spec keeps the manifest")

		Expect(registry.SetManifest(id, manifest("tail"))).To(Succeed())
		Expect(changes).To(Equal(2))

		registry.Remove(id)
		Expect(registry.Operations()).To(BeEmpty())
		Expect(changes).To(Equal(3))
	})

	ginkgo.It("reports operations of proxied plugins", func() {
		_, err := registry.UpsertProxied(uuid.New(), "agent", "remote", v1.PluginSpec{}, manifest("run"), uuid.New())
		Expect(err).ToNot(HaveOccurred())
		Expect(registry.Operations()).To(HaveKeyWithValue("remote", []string{"run"}))
		Expect(changes).To(Equal(1))
	})
})
