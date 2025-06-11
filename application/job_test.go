package application

import (
	"encoding/json"
	"os"

	"github.com/flanksource/duty/models"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

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
		Expect(j.LastJob.SuccessCount).To(Equal(1))
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
