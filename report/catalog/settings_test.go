package catalog

import (
	"os"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Settings", func() {
	ginkgo.Describe("LoadSettings", func() {
		ginkgo.It("parses valid YAML", func() {
			content := `
filters:
  - "type!=Kubernetes::ConfigMap"
  - "type!=Kubernetes::Secret"
thresholds:
  staleDays: 60
  reviewOverdueDays: 30
categoryMappings:
  - category: rbac.granted
    filter: 'changeType == "PermissionGranted" || changeType == "PermissionAdded"'
  - category: backup.failed
    filter: 'changeType == "BackupFailed"'
`
			path := filepath.Join(os.TempDir(), "test-settings.yaml")
			Expect(os.WriteFile(path, []byte(content), 0600)).To(Succeed())
			defer os.Remove(path)

			s, err := LoadSettings(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(s.Filters).To(Equal([]string{"type!=Kubernetes::ConfigMap", "type!=Kubernetes::Secret"}))
			Expect(s.Thresholds.StaleDays).To(Equal(60))
			Expect(s.Thresholds.ReviewOverdueDays).To(Equal(30))
			Expect(s.CategoryMappings).To(HaveLen(2))
			Expect(s.CategoryMappings[0].Category).To(Equal("rbac.granted"))
			Expect(s.CategoryMappings[0].Filter).To(Equal(`changeType == "PermissionGranted" || changeType == "PermissionAdded"`))
			Expect(s.CategoryMappings[1].Category).To(Equal("backup.failed"))
			Expect(s.CategoryMappings[1].Filter).To(Equal(`changeType == "BackupFailed"`))
		})

		ginkgo.It("returns error for missing file", func() {
			_, err := LoadSettings("/nonexistent/path.yaml")
			Expect(err).To(HaveOccurred())
		})

		ginkgo.It("rejects the old category mapping shape", func() {
			content := `
categoryMappings:
  backup.failed:
    - BackupFailed
`
			path := filepath.Join(os.TempDir(), "legacy-settings.yaml")
			Expect(os.WriteFile(path, []byte(content), 0600)).To(Succeed())
			defer os.Remove(path)

			_, err := LoadSettings(path)
			Expect(err).To(HaveOccurred())
		})
	})

	ginkgo.Describe("LoadDefaultSettings", func() {
		ginkgo.It("loads embedded defaults", func() {
			s, err := LoadDefaultSettings()
			Expect(err).ToNot(HaveOccurred())
			Expect(s.Filters).To(ContainElement("type!=Kubernetes::ConfigMap"))
			Expect(s.Thresholds.StaleDays).To(Equal(90))
			Expect(s.Thresholds.ReviewOverdueDays).To(Equal(90))
			Expect(s.CategoryMappings).ToNot(BeEmpty())
			Expect(s.CategoryMappings[0].Category).To(Equal("rbac.granted"))
			Expect(s.CategoryMappings[0].Filter).To(ContainSubstring("PermissionGranted"))
			Expect(s.CategoryMappings).To(ContainElement(HaveField("Category", "deployment.failed")))
		})
	})

	ginkgo.Describe("ResolveSettings", func() {
		ginkgo.It("uses embedded defaults when no path is provided", func() {
			s, source, err := ResolveSettings("")
			Expect(err).ToNot(HaveOccurred())
			Expect(source).To(Equal(EmbeddedSettingsSource))
			Expect(s.Filters).To(ContainElement("type!=Kubernetes::Secret"))
			Expect(s.CategoryMappings).To(ContainElement(HaveField("Category", "backup.failed")))
		})

		ginkgo.It("overlays file settings on top of embedded defaults", func() {
			content := `
filters:
  - "name=test"
thresholds:
  staleDays: 60
categoryMappings:
  - category: backup.failed
    filter: 'changeType == "BACKUP_DB" && severity == "high"'
  - category: deployment.failed
    filter: 'changeType == "CodeDeployment" && severity == "failed"'
`
			path := filepath.Join(os.TempDir(), "overlay-settings.yaml")
			Expect(os.WriteFile(path, []byte(content), 0600)).To(Succeed())
			defer os.Remove(path)

			s, source, err := ResolveSettings(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(strings.Contains(source, EmbeddedSettingsSource)).To(BeTrue())
			Expect(strings.Contains(source, path)).To(BeTrue())
			Expect(s.Filters).To(Equal([]string{"name=test"}))
			Expect(s.Thresholds.StaleDays).To(Equal(60))
			Expect(s.Thresholds.ReviewOverdueDays).To(Equal(90))
			Expect(s.CategoryMappings).To(HaveLen(2))
			Expect(s.CategoryMappings[0].Category).To(Equal("backup.failed"))
			Expect(s.CategoryMappings[0].Filter).To(Equal(`changeType == "BACKUP_DB" && severity == "high"`))
			Expect(s.CategoryMappings[1].Category).To(Equal("deployment.failed"))
			Expect(s.CategoryMappings[1].Filter).To(Equal(`changeType == "CodeDeployment" && severity == "failed"`))
		})
	})

	ginkgo.Describe("FilterQuery", func() {
		ginkgo.It("joins filters into search string", func() {
			s := &Settings{Filters: []string{"type!=Kubernetes::ConfigMap", "type!=Kubernetes::Secret"}}
			Expect(s.FilterQuery()).To(Equal("type!=Kubernetes::ConfigMap type!=Kubernetes::Secret"))
		})

		ginkgo.It("returns empty for nil settings", func() {
			var s *Settings
			Expect(s.FilterQuery()).To(Equal(""))
		})

		ginkgo.It("returns empty for no filters", func() {
			s := &Settings{}
			Expect(s.FilterQuery()).To(Equal(""))
		})
	})

	ginkgo.Describe("Options threshold methods", func() {
		ginkgo.It("returns defaults when no settings", func() {
			opts := Options{}
			Expect(opts.StaleDays()).To(Equal(90))
			Expect(opts.ReviewOverdueDays()).To(Equal(90))
		})

		ginkgo.It("returns settings values", func() {
			opts := Options{
				Settings: &Settings{
					Thresholds: SettingsThresholds{StaleDays: 60, ReviewOverdueDays: 30},
				},
			}
			Expect(opts.StaleDays()).To(Equal(60))
			Expect(opts.ReviewOverdueDays()).To(Equal(30))
		})

		ginkgo.It("returns defaults when settings thresholds are zero", func() {
			opts := Options{Settings: &Settings{}}
			Expect(opts.StaleDays()).To(Equal(90))
			Expect(opts.ReviewOverdueDays()).To(Equal(90))
		})
	})
})
