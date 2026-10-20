package views

import (
	"encoding/json"
	"fmt"
	"testing"

	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
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

	// Built-in roles grant nothing, so John Doe reads components and playbooks, and uses connections, through a Role.
	// It grants no read on configs: a Scope selecting configs is stamped on every config row of a view, in its
	// grants column, which the tests compare.
	const namespace = "views-suite"
	uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }
	var rules []v1.RoleRule
	for _, kind := range []string{"component", "playbook", "connection"} {
		var target v1.ScopeTarget
		Expect(json.Unmarshal(fmt.Appendf(nil, `{%q: {"name": "*"}}`, kind), &target)).To(Succeed())
		Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{
			ObjectMeta: metav1.ObjectMeta{Name: kind, Namespace: namespace, UID: uid()},
			Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{target}},
		})).To(Succeed())
		rules = append(rules, v1.RoleRule{Name: "read-" + kind, Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: kind}})
	}
	rules = append(rules, v1.RoleRule{Name: "use-connection", Action: adapter.ActionConnectionUse, Resource: &v1.ScopeReference{ScopeRef: "connection"}})
	Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "john-doe", Namespace: namespace, UID: uid()},
		Spec:       v1.RoleSpec{Rules: rules},
	})).To(Succeed())
	Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "john-doe", Namespace: namespace, UID: uid()},
		Spec: v1.RoleBindingSpec{Role: "john-doe", Subjects: v1.RoleBindingSubjects{
			PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{dummy.JohnDoe.Email}},
		}},
	})).To(Succeed())
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
