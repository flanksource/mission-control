package push

import (
	"testing"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/setup"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	DefaultContext context.Context

	shutdown func()
)

func TestPush(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Push")
}

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn(setup.WithoutDummyData)
})

var _ = ginkgo.AfterSuite(func() {
	if shutdown != nil {
		shutdown()
	}

	setup.AfterSuiteFn()
})
