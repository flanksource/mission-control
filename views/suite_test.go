package views

import (
	"fmt"
	"testing"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

func TestViews(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Views")
}

var (
	DefaultContext context.Context
	conn           models.Connection
	connectionName string
)

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn()
	Expect(dutyRBAC.Init(DefaultContext, []string{dummy.JohnDoe.ID.String()}, adapter.NewPermissionAdapter)).To(Succeed())
	DefaultContext = DefaultContext.WithUser(&dummy.JohnDoe)

	conn = models.Connection{
		Name:      "test-db-connection",
		Namespace: "default",
		Type:      models.ConnectionTypePostgres,
		URL:       DefaultContext.Value("db_url").(string),
		Source:    models.SourceUI,
	}
	Expect(DefaultContext.DB().Save(&conn).Error).ToNot(HaveOccurred())

	connectionName = fmt.Sprintf("connection://%s/%s", conn.Namespace, conn.Name)
})

var _ = ginkgo.AfterSuite(func() {
	Expect(DefaultContext.DB().Delete(&conn).Error).ToNot(HaveOccurred())
	setup.AfterSuiteFn()
})
