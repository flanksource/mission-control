package db

import (
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = ginkgo.Describe("Plugin CRD persistence", func() {
	ginkgo.Describe("PluginFromCRD round-trip", func() {
		ginkgo.It("preserves every spec field through encode and decode", func() {
			id := uuid.New()
			selector := types.ResourceSelector{
				Name:      "kube-pod",
				Namespace: "kube-system",
				Types:     types.Items{"Kubernetes::Pod", "Kubernetes::Deployment"},
			}
			connections := v1.PluginConnectionMappings{
				Types: map[string]string{
					"kubernetes": "connection://default/kubernetes",
					"sql":        "connection://default/postgres",
				},
				Labels: map[string]string{
					"logs": "connection://default/loki",
				},
			}
			crd := &v1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kubernetes-logs",
					Namespace: "default",
					UID:       k8stypes.UID(id.String()),
				},
				Spec: v1.PluginSpec{
					Source:      "github.com/flanksource/plugin-k8s-logs",
					Version:     "v0.4.2",
					Checksum:    "sha256:cafef00d",
					Selector:    selector,
					Connections: connections,
					Audit:       []string{"logs-*", "!debug"},
					Properties:  map[string]string{"region": "us-east-1", "log_level": "debug"},
				},
			}

			row, err := PluginFromCRD(crd)
			Expect(err).ToNot(HaveOccurred())
			Expect(row.ID).To(Equal(id))
			Expect(row.Name).To(Equal("kubernetes-logs"))
			Expect(row.Namespace).To(Equal("default"))
			Expect(row.Source).To(Equal(models.SourceCRD))
			Expect(string(row.Spec)).To(ContainSubstring(`"source":"github.com/flanksource/plugin-k8s-logs"`))
			Expect(string(row.Spec)).To(ContainSubstring(`"name":"kube-pod"`))
			Expect(string(row.Spec)).To(ContainSubstring("connection://default/postgres"))
			Expect(string(row.Spec)).To(ContainSubstring(`"audit":["logs-*","!debug"]`))

			spec, err := PluginToSpec(row)
			Expect(err).ToNot(HaveOccurred())
			Expect(spec.Source).To(Equal(crd.Spec.Source))
			Expect(spec.Version).To(Equal(crd.Spec.Version))
			Expect(spec.Checksum).To(Equal(crd.Spec.Checksum))
			Expect(spec.Selector).To(Equal(crd.Spec.Selector))
			Expect(spec.Connections).To(Equal(crd.Spec.Connections))
			Expect(spec.Audit).To(Equal(crd.Spec.Audit))
			Expect(spec.Properties).To(Equal(crd.Spec.Properties))
		})

		ginkgo.It("rejects an invalid UID", func() {
			crd := &v1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bad-uid",
					UID:  k8stypes.UID("not-a-uuid"),
				},
				Spec: v1.PluginSpec{Source: "x"},
			}
			_, err := PluginFromCRD(crd)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse uid"))
		})
	})

	ginkgo.Describe("Soft-delete callbacks", ginkgo.Ordered, func() {
		var staleUID, otherUID uuid.UUID
		newerUID := uuid.New()
		const name = "stale-test-plugin"
		const namespace = "default"

		ginkgo.BeforeAll(func() {
			staleUID = uuid.New()
			otherUID = uuid.New()
			rows := []models.Plugin{
				{ID: staleUID, Name: name, Namespace: namespace, Spec: types.JSON(`{"source":"old"}`), Source: models.SourceCRD},
				{ID: otherUID, Name: "other-plugin", Namespace: namespace, Spec: types.JSON(`{"source":"x"}`), Source: models.SourceCRD},
			}
			for i := range rows {
				Expect(DefaultContext.DB().Save(&rows[i]).Error).ToNot(HaveOccurred())
			}
		})

		ginkgo.AfterAll(func() {
			DefaultContext.DB().Unscoped().Where("namespace = ? AND name IN ?", namespace, []string{name, "other-plugin"}).Delete(&models.Plugin{})
		})

		ginkgo.It("DeleteStalePlugin only soft-deletes rows with the same name/namespace and a different UID", func() {
			newer := &v1.Plugin{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8stypes.UID(newerUID.String())},
				Spec:       v1.PluginSpec{Source: "new"},
			}
			Expect(DeleteStalePlugin(DefaultContext, newer)).To(Succeed())

			var stale models.Plugin
			Expect(DefaultContext.DB().Where("id = ?", staleUID).First(&stale).Error).ToNot(HaveOccurred())
			Expect(stale.DeletedAt).ToNot(BeNil())

			var other models.Plugin
			Expect(DefaultContext.DB().Where("id = ?", otherUID).First(&other).Error).ToNot(HaveOccurred())
			Expect(other.DeletedAt).To(BeNil())

			plugins, err := ListPlugins(DefaultContext)
			Expect(err).ToNot(HaveOccurred())
			names := map[string]int{}
			for _, p := range plugins {
				if p.Namespace == namespace {
					names[p.Name]++
				}
			}
			Expect(names[name]).To(Equal(0))
			Expect(names["other-plugin"]).To(Equal(1))
		})

		ginkgo.It("DeletePlugin soft-deletes the surviving row by id", func() {
			Expect(DeletePlugin(DefaultContext, otherUID.String())).To(Succeed())

			var row models.Plugin
			Expect(DefaultContext.DB().Where("id = ?", otherUID).First(&row).Error).ToNot(HaveOccurred())
			Expect(row.DeletedAt).ToNot(BeNil())
		})
	})
})
