package application

import (
	"encoding/json"
	"os"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

var _ = ginkgo.Describe("Application Sections", ginkgo.Ordered, func() {
	var app v1.Application

	ginkgo.BeforeAll(func() {
		backupsView := readView("testdata/backups-view.yaml")
		Expect(db.PersistViewFromCRD(DefaultContext, &backupsView)).To(Succeed())

		appData := dummy.GetApplicationDummyData()
		Expect(appData.Populate(DefaultContext)).To(Succeed())

		app = readApplication("testdata/incident-commander.yaml")
		Expect(db.PersistApplicationFromCRD(DefaultContext, &app)).To(Succeed())
	})

	ginkgo.It("incident-commander application run", func() {
		generated, err := buildApplication(DefaultContext, &app)
		Expect(err).ToNot(HaveOccurred())

		out, err := json.MarshalIndent(generated, "", "  ")
		Expect(err).ToNot(HaveOccurred())
		ginkgo.GinkgoWriter.Println(string(out))

		Expect(generated.Sections).To(HaveLen(3))

		viewSection := generated.Sections[0]
		Expect(viewSection.Type).To(Equal(api.SectionTypeView))
		Expect(viewSection.Title).To(Equal("Backups"))
		Expect(viewSection.View).ToNot(BeNil())
		Expect(viewSection.Changes).To(BeNil())
		Expect(viewSection.Configs).To(BeNil())

		changesSection := generated.Sections[1]
		Expect(changesSection.Type).To(Equal(api.SectionTypeChanges))
		Expect(changesSection.Title).To(Equal("Recent Changes"))
		Expect(changesSection.View).To(BeNil())
		Expect(changesSection.Changes).ToNot(BeNil())
		Expect(changesSection.Configs).To(BeNil())

		configsSection := generated.Sections[2]
		Expect(configsSection.Type).To(Equal(api.SectionTypeConfigs))
		Expect(configsSection.Title).To(Equal("Deployments"))
		Expect(configsSection.View).To(BeNil())
		Expect(configsSection.Changes).To(BeNil())
		Expect(configsSection.Configs).ToNot(BeNil())
	})

	ginkgo.It("view section data must not contain UI-only fields", func() {
		generated, err := buildApplication(DefaultContext, &app)
		Expect(err).ToNot(HaveOccurred())

		Expect(generated.Sections[0].View).To(BeAssignableToTypeOf(&api.ApplicationViewData{}))
	})

	ginkgo.It("should have backups and restores from mock data", func() {
		generated, err := buildApplication(DefaultContext, &app)
		Expect(err).ToNot(HaveOccurred())

		Expect(generated.Backups).To(HaveLen(2))
		// Most recent backup first (slices.Reverse applied after dedupBackupChanges)
		Expect(generated.Backups[0].Database).To(Equal("incident-commander-db"))
		Expect(generated.Backups[0].Size).To(Equal("4.3GB"))
		Expect(generated.Backups[0].Status).To(Equal("success"))
		Expect(generated.Backups[1].Database).To(Equal("incident-commander-db"))
		Expect(generated.Backups[1].Size).To(Equal("4.2GB"))
		Expect(generated.Backups[1].Status).To(Equal("success"))

		Expect(generated.Restores).To(HaveLen(1))
		Expect(generated.Restores[0].Database).To(Equal("incident-commander-db"))
		Expect(generated.Restores[0].Status).To(Equal("success"))
	})

	ginkgo.It("should have access control users from mock data", func() {
		generated, err := buildApplication(DefaultContext, &app)
		Expect(err).ToNot(HaveOccurred())

		Expect(generated.AccessControl.Users).To(HaveLen(2))
		names := []string{generated.AccessControl.Users[0].Name, generated.AccessControl.Users[1].Name}
		Expect(names).To(ConsistOf("Alice", "Bob"))
	})

	ginkgo.It("should export application as PDF", func() {
		generated, err := buildApplication(DefaultContext, &app)
		Expect(err).ToNot(HaveOccurred())

		pdfBytes, err := RenderPDF(generated)
		Expect(err).ToNot(HaveOccurred())
		Expect(pdfBytes).ToNot(BeEmpty())

		outFile, err := os.CreateTemp("", "application-export-*.pdf")
		Expect(err).ToNot(HaveOccurred())
		defer os.Remove(outFile.Name())

		Expect(os.WriteFile(outFile.Name(), pdfBytes, 0644)).To(Succeed())
		ginkgo.GinkgoWriter.Printf("PDF written to %s (%d bytes)\n", outFile.Name(), len(pdfBytes))
	})
})

var _ = ginkgo.Describe("Application Job", ginkgo.Ordered, func() {
	var azureApp v1.Application
	var azureModel *models.Application

	ginkgo.BeforeAll(func() {
		azureApp = readApplication("testdata/azure.yaml")
		Expect(db.PersistApplicationFromCRD(DefaultContext, &azureApp)).To(Succeed())

		var err error
		azureModel, err = db.FindApplication(DefaultContext, azureApp.Namespace, azureApp.Name)
		Expect(err).ToNot(HaveOccurred())
		Expect(azureModel).ToNot(BeNil())
		Expect(azureModel.ID.String()).To(Equal(azureApp.GetID().String()))

		{
			scrapeConfig := readScrapeConfig("testdata/azure-scrapeconfig.yaml")
			Expect(DefaultContext.DB().Save(&scrapeConfig).Error).To(Succeed())

			// Create Azure::EnterpriseApplication named "the-application"
			enterpriseApplication := models.ConfigItem{
				Name: lo.ToPtr("the-application"),
				Type: lo.ToPtr("Azure::EnterpriseApplication"),
				Tags: map[string]string{
					"namespace": "mc",
				},
				ScraperID: lo.ToPtr(scrapeConfig.ID.String()),
				Config:    lo.ToPtr(`{"type": "Azure::EnterpriseApplication"}`),
			}
			Expect(DefaultContext.DB().Save(&enterpriseApplication).Error).To(Succeed())
		}
	})

	ginkgo.It("should run the sync job", func() {
		j := SyncApplications(DefaultContext)
		j.Run()

		Expect(j.LastJob.ErrorCount).To(BeZero())
		Expect(j.LastJob.Errors).To(HaveLen(0))
		Expect(j.LastJob.SuccessCount).To(BeNumerically(">=", 1))
	})

	ginkgo.It("should have created the config scraper for azure", func() {
		var scraper models.ConfigScraper
		Expect(DefaultContext.DB().Where("application_id = ?", azureModel.ID).
			Where("source =?", models.SourceApplicationCRD).
			First(&scraper).Error).To(Succeed())
		Expect(scraper.ID).ToNot(BeEmpty())
		Expect(scraper.ApplicationID.String()).To(Equal(azureModel.ID.String()))
	})

	ginkgo.It("should have created the custom roles", func() {
		var roles []models.ExternalRole
		Expect(DefaultContext.DB().Where("application_id = ?", azureModel.ID).Order("name").Find(&roles).Error).To(Succeed())
		Expect(roles).To(HaveLen(2))

		Expect(roles[0].Name).To(Equal("Admin"))
		Expect(roles[1].Name).To(Equal("User"))
	})
})

func readApplication(path string) v1.Application {
	content, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred())

	var application v1.Application
	Expect(yaml.Unmarshal(content, &application)).To(Succeed())

	return application
}

func readView(path string) v1.View {
	content, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred())

	var view v1.View
	Expect(yaml.Unmarshal(content, &view)).To(Succeed())

	return view
}

func readScrapeConfig(path string) models.ConfigScraper {
	content, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred())

	var mm map[string]any
	Expect(yaml.Unmarshal(content, &mm)).To(Succeed())

	var scrapeConfig unstructured.Unstructured
	scrapeConfig.SetUnstructuredContent(mm)

	spec, ok := scrapeConfig.Object["spec"].(map[string]any)
	Expect(ok).To(BeTrue())

	marshalled, err := json.Marshal(spec)
	Expect(err).ToNot(HaveOccurred())

	scraper := models.ConfigScraper{
		ID:        uuid.MustParse(string(scrapeConfig.GetUID())),
		Namespace: scrapeConfig.GetNamespace(),
		Name:      scrapeConfig.GetName(),
		Spec:      string(marshalled),
		Source:    models.SourceCRD,
	}
	return scraper
}
