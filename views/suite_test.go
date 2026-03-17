package views

import (
	"fmt"
	"testing"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/setup"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
