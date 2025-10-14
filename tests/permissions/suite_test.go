package permissions_test

import (
	"testing"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/setup"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPermissions(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Permissions")
}

var (
	DefaultContext context.Context
)

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn()
})

var _ = ginkgo.AfterSuite(setup.AfterSuiteFn)
