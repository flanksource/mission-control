package views

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

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
		if !strings.HasSuffix(file.Name(), ".yaml") {
			continue
		}

		It(file.Name(), func() {
			viewObj, err := loadViewFromYAML(file.Name())
			Expect(err).ToNot(HaveOccurred())

			_, err = PopulateView(DefaultContext, viewObj)
			Expect(err).ToNot(HaveOccurred())

			tableName := viewObj.TableName()
			Expect(DefaultContext.DB().Migrator().HasTable(tableName)).To(BeTrue())

			rows, err := db.ReadViewTable(DefaultContext, tableName)
			Expect(err).ToNot(HaveOccurred())

			expectedRowsAnnotation, exists := viewObj.Annotations["expected-rows"]
			Expect(exists).To(BeTrue(), "expected-rows annotation not found for view: %s", viewObj.Name)

			var expectedRows [][]any
			err = json.Unmarshal([]byte(expectedRowsAnnotation), &expectedRows)
			Expect(err).ToNot(HaveOccurred())

			Expect(rows).To(HaveLen(len(expectedRows)))

			expectedResults := make([]api.ViewRow, len(expectedRows))
			for i, row := range expectedRows {
				expectedResults[i] = api.ViewRow(row)
			}
			Expect(rows).To(Equal(expectedResults))
		})
	}
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
