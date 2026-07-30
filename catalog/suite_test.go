package catalog

import (
	"testing"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/setup"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCatalog(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Catalog")
}

var DefaultContext context.Context

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn(setup.WithoutDummyData)
})

var _ = ginkgo.AfterSuite(func() {
	setup.AfterSuiteFn()
})
