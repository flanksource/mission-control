package views

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	pkgView "github.com/flanksource/duty/view"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

var _ = Describe("View Database Table", func() {
	testdataDir := "testdata"
	files, err := os.ReadDir(testdataDir)
	Expect(err).ToNot(HaveOccurred())

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".yaml") || strings.HasPrefix(file.Name(), "cache-test-") {
			continue
		}

		It(file.Name(), func() {
			viewObj, err := loadViewFromYAML(file.Name())
			Expect(err).ToNot(HaveOccurred())

			err = db.PersistViewFromCRD(DefaultContext, viewObj)
			Expect(err).ToNot(HaveOccurred())

			// save the results to DB first so ReadOrPopulateViewTable reads them
			_, err = populateView(DefaultContext, viewObj, false)
			Expect(err).ToNot(HaveOccurred())

			// Verify that lastRan field is populated after PopulateView
			var dbView models.View
			err = DefaultContext.DB().Where("name = ? AND namespace = ?", viewObj.Name, viewObj.Namespace).First(&dbView).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(dbView.LastRan).ToNot(BeNil(), "lastRan field should be populated after PopulateView")

			tableName := viewObj.TableName()
			Expect(DefaultContext.DB().Migrator().HasTable(tableName)).To(BeTrue())

			result, err := ReadOrPopulateViewTable(DefaultContext.WithUser(&dummy.JohnDoe), viewObj.Namespace, viewObj.Name, WithIncludeRows(true))
			Expect(err).ToNot(HaveOccurred())

			expectedRowsAnnotation, exists := viewObj.Annotations["expected-rows"]
			Expect(exists).To(BeTrue(), "expected-rows annotation not found for view: %s", viewObj.Name)

			var expectedRows [][]any
			err = json.Unmarshal([]byte(expectedRowsAnnotation), &expectedRows)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Rows).To(HaveLen(len(expectedRows)))

			expectedResults := make([]pkgView.Row, len(expectedRows))
			for i, row := range expectedRows {
				expectedResults[i] = pkgView.Row(row)
			}
			Expect(result.Rows).To(Equal(expectedResults))

			expectedPanelsAnnotation, exists := viewObj.Annotations["expected-panels"]
			if exists {
				var expectedPanels []api.PanelResult
				err = json.Unmarshal([]byte(expectedPanelsAnnotation), &expectedPanels)
				Expect(err).ToNot(HaveOccurred())

				Expect(result.Panels).To(HaveLen(len(expectedPanels)))

				// Convert both to JSON and back to normalize types (int64 vs float64)
				actualJSON, err := json.Marshal(result.Panels)
				Expect(err).ToNot(HaveOccurred())

				expectedJSON, err := json.Marshal(expectedPanels)
				Expect(err).ToNot(HaveOccurred())

				Expect(string(actualJSON)).To(Equal(string(expectedJSON)))
			}
		})
	}
})

var _ = Describe("ReadOrPopulateViewTable Cache Control", func() {
	It("should return an error when refresh-timeout is very low", func() {
		viewObj, err := loadViewFromYAML("cache-test-view.yaml")
		Expect(err).ToNot(HaveOccurred())

		err = db.PersistViewFromCRD(DefaultContext, viewObj)
		Expect(err).ToNot(HaveOccurred())

		tableName := viewObj.TableName()
		if DefaultContext.DB().Migrator().HasTable(tableName) {
			err = DefaultContext.DB().Migrator().DropTable(tableName)
			Expect(err).ToNot(HaveOccurred())
		}

		_, err = ReadOrPopulateViewTable(DefaultContext.WithUser(&dummy.JohnDoe), viewObj.Namespace, viewObj.Name, WithRefreshTimeout(1*time.Microsecond))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("refresh timeout reached. try again"))
	})
})

// loadViewFromYAML loads a View from a YAML fixture file
func loadViewFromYAML(filename string) (*v1.View, error) {
	yamlPath := filepath.Join("testdata", filename)
	yamlData, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, err
	}

	var view v1.View
	err = yaml.Unmarshal(yamlData, &view)
	if err != nil {
		return nil, err
	}

	return &view, nil
}
