package application

import (
	"fmt"
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/samber/oops"

	"github.com/flanksource/duty/context"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"

	"github.com/flanksource/incident-commander/rbac/adapter"

	// register event handlers
	_ "github.com/flanksource/incident-commander/notification"
)

func TestApplication(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Application")
}

var DefaultContext context.Context

var _ = ginkgo.BeforeSuite(func() {
	format.RegisterCustomFormatter(func(value any) (string, bool) {
		switch v := value.(type) {
		case error:
			if err, ok := oops.AsOops(v); ok {
				return fmt.Sprintf("%+v", err), true
			}
		}

		return "", false
	})

	DefaultContext = setup.BeforeSuiteFn()
	DefaultContext.Logger.SetLogLevel(DefaultContext.Properties().String("log.level", "info"))
	DefaultContext.Infof("%s", DefaultContext.String())

	Expect(dutyRBAC.Init(DefaultContext, []string{dummy.JohnDoe.ID.String()}, adapter.NewPermissionAdapter)).To(Succeed())
	DefaultContext = DefaultContext.WithUser(&dummy.JohnDoe)
})

var _ = ginkgo.AfterSuite(func() {
	setup.AfterSuiteFn()
})
