package auth

import (
	"testing"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/incident-commander/auth/signing"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAuth(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Auth")
}

var (
	DefaultContext context.Context
)

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn()
	if _, _, err := signing.Initialize("/tmp/dummy"); err != nil {
		ginkgo.Fail(err.Error())
	}
})

var _ = ginkgo.AfterSuite(setup.AfterSuiteFn)
