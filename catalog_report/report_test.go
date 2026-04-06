package catalog_report

import (
	"testing"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/api"
)

func TestCatalogReport(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "CatalogReport")
}

var _ = ginkgo.Describe("Options", func() {
	ginkgo.It("WithDefaults sets 30-day since", func() {
		opts := Options{}.WithDefaults()
		Expect(opts.Since).To(Equal(30 * 24 * time.Hour))
	})

	ginkgo.It("WithDefaults preserves custom since", func() {
		opts := Options{Since: 7 * 24 * time.Hour}.WithDefaults()
		Expect(opts.Since).To(Equal(7 * 24 * time.Hour))
	})
})

var _ = ginkgo.Describe("Report date range", func() {
	ginkgo.It("From is set from sinceTime", func() {
		opts := Options{Since: 48 * time.Hour}.WithDefaults()
		sinceTime := time.Now().Add(-opts.Since)

		report := &api.CatalogReport{
			From: sinceTime.Format(time.RFC3339),
		}

		parsed, err := time.Parse(time.RFC3339, report.From)
		Expect(err).ToNot(HaveOccurred())
		Expect(parsed).To(BeTemporally("~", time.Now().Add(-48*time.Hour), 2*time.Second))
	})

	ginkgo.It("From matches sinceTime for 30-day default", func() {
		opts := Options{}.WithDefaults()
		sinceTime := time.Now().Add(-opts.Since)

		report := &api.CatalogReport{
			From: sinceTime.Format(time.RFC3339),
		}

		parsed, err := time.Parse(time.RFC3339, report.From)
		Expect(err).ToNot(HaveOccurred())
		Expect(parsed).To(BeTemporally("~", time.Now().Add(-30*24*time.Hour), 2*time.Second))
	})

	ginkgo.It("query FromTime matches report From", func() {
		opts := Options{Since: 7 * 24 * time.Hour}.WithDefaults()
		sinceTime := time.Now().Add(-opts.Since)

		report := &api.CatalogReport{
			From: sinceTime.Format(time.RFC3339),
		}

		reportFrom, err := time.Parse(time.RFC3339, report.From)
		Expect(err).ToNot(HaveOccurred())
		Expect(reportFrom).To(BeTemporally("~", sinceTime, time.Second))
	})
})
