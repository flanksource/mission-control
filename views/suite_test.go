package views

import (
	"fmt"
	"testing"

	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	icrbac "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
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
	Expect(icrbac.Init(DefaultContext)).To(Succeed())
	DefaultContext = DefaultContext.WithUser(&dummy.JohnDoe)

	// Built-in roles grant nothing, so John Doe reads what the views query, and uses their connections, through
	// Permissions. Unlike the shipped Scopes, they add no Scope to the rows' grants.
	wildcard := []types.ResourceSelector{{Name: "*"}}
	for kind, selectors := range map[string]dutyRBAC.Selectors{
		"configs":     {Configs: wildcard},
		"components":  {Components: wildcard},
		"playbooks":   {Playbooks: wildcard},
		"connections": {Connections: wildcard},
	} {
		Expect(db.PersistPermissionFromCRD(DefaultContext, &v1.Permission{
			ObjectMeta: metav1.ObjectMeta{Name: "views-suite-john-doe-" + kind, Namespace: "default", UID: k8sTypes.UID(uuid.NewString())},
			Spec: v1.PermissionSpec{
				Subject: v1.PermissionSubject{Person: dummy.JohnDoe.Email},
				Actions: []string{policy.ActionRead, adapter.ActionConnectionUse},
				Object:  v1.PermissionObject{Selectors: selectors},
			},
		})).To(Succeed())
	}
	Expect(dutyRBAC.ReloadPolicy()).To(Succeed())

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
