package metrics

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/setup"
)

func TestMetrics(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Metrics")
}

var DefaultContext context.Context

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn()
})

var _ = ginkgo.AfterSuite(func() {
	setup.AfterSuiteFn()
})
