package application

import (
	"strings"
	"time"

	"github.com/flanksource/duty/context"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	icapi "github.com/flanksource/incident-commander/api"
)

func testApp() *icapi.Application {
	return &icapi.Application{
		ApplicationDetail: icapi.ApplicationDetail{
			ID:        "test-id",
			Name:      "test-app",
			Type:      "Platform",
			Namespace: "default",
			CreatedAt: time.Now(),
		},
		Incidents: []icapi.ApplicationIncident{},
		Backups:   []icapi.ApplicationBackup{},
		Restores:  []icapi.ApplicationBackupRestore{},
		Findings:  []icapi.ApplicationFinding{},
		Sections:  []icapi.ApplicationSection{},
		Locations: []icapi.ApplicationLocation{},
	}
}

var _ = ginkgo.Describe("RenderFacetHTML", ginkgo.Label("ignore_local"), func() {
	ginkgo.It("should render HTML output", func() {
		ginkgo.Skip("disabled: facet integration test is flaky in CI")

		data, err := RenderFacetHTML(context.New(), testApp())
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(ContainSubstring("<html"))
	})
})

var _ = ginkgo.Describe("RenderFacetPDF", ginkgo.Label("ignore_local"), func() {
	ginkgo.It("should render PDF output", func() {
		ginkgo.Skip("disabled: facet integration test is flaky in CI")

		data, err := RenderFacetPDF(context.New(), testApp())
		if err != nil && strings.Contains(err.Error(), "Failed to launch the browser") {
			ginkgo.Skip("headless Chrome unavailable in this environment; skipping facet-pdf test")
		}
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(HavePrefix("%PDF"))
	})
})
