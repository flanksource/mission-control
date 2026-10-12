package plugin

import (
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/plugin/api"
)

var _ = ginkgo.Describe("Registry operations", func() {
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
		registry.OnOperationsChanged(func() { changes++ })
		id = uuid.New()
		_, err := registry.Upsert(id, "default", "logs", v1.PluginSpec{Source: "test"})
		Expect(err).ToNot(HaveOccurred())
	})

	ginkgo.It("doesn't report a plugin without a manifest", func() {
		Expect(registry.Operations()).To(BeEmpty())
		Expect(changes).To(BeZero())
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
