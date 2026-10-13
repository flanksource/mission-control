package auth

import (
	"fmt"
	"os"

	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/bcrypt"

	"github.com/flanksource/incident-commander/api"
	icrbac "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/vars"
)

var _ = ginkgo.Describe("Built-in roles stored by an earlier version", func() {
	ginkgo.It("grant nothing after an upgrade, and are deleted", func() {
		oldAuthMode, oldOIDCEnabled, oldHtpasswdFile := vars.AuthMode, OIDCEnabled, HtpasswdFile
		oldSystemUserID, oldLocalhostOnly, oldChecker := api.SystemUserID, localhostOnly, checker
		ginkgo.DeferCleanup(func() {
			vars.AuthMode, OIDCEnabled, HtpasswdFile = oldAuthMode, oldOIDCEnabled, oldHtpasswdFile
			api.SystemUserID, localhostOnly, checker = oldSystemUserID, oldLocalhostOnly, oldChecker
		})

		var system models.Person
		Expect(DefaultContext.DB().Where("name = 'System'").First(&system).Error).To(Succeed())
		api.SystemUserID = &system.ID

		person := models.Person{ID: uuid.New(), Name: "Legacy Admin", Email: "legacy-admin@example.com"}
		Expect(DefaultContext.DB().Create(&person).Error).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(DefaultContext.DB().Delete(&person).Error).To(Succeed())
		})

		// What earlier versions stored: built-in role assignments, and the roles' own rules from duty's policies.yaml
		legacy := []gormadapter.CasbinRule{
			{Ptype: "g", V0: system.ID.String(), V1: policy.RoleAdmin},
			{Ptype: "g", V0: person.ID.String(), V1: policy.RoleAdmin},
			{Ptype: "g", V0: person.ID.String(), V1: policy.RoleEditor},
			{Ptype: "g", V0: person.ID.String(), V1: policy.RoleEveryone},
			{Ptype: "g", V0: policy.RoleAdmin, V1: policy.RoleEveryone},
			{Ptype: "p", V0: policy.RoleAdmin, V1: "*", V2: "*", V3: "allow", V5: "na"},
			{Ptype: "p", V0: policy.RoleEditor, V1: policy.ObjectCatalog, V2: policy.ActionRead, V3: "allow", V5: "na"},
		}
		Expect(DefaultContext.DB().Create(&legacy).Error).To(Succeed())

		hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		Expect(err).ToNot(HaveOccurred())
		htpasswdPath := ginkgo.GinkgoT().TempDir() + "/htpasswd"
		Expect(os.WriteFile(htpasswdPath, fmt.Appendf(nil, "admin:%s\n", string(hash)), 0600)).To(Succeed())
		vars.AuthMode = Basic
		OIDCEnabled = false
		HtpasswdFile = htpasswdPath

		Expect(Middleware(DefaultContext, newEchoInstance(DefaultContext))).To(Succeed())

		var stored int64
		Expect(DefaultContext.DB().Table("casbin_rule").
			Where("v0 IN ? OR v1 IN ?", []string{policy.RoleAdmin, policy.RoleEditor, policy.RoleEveryone}, []string{policy.RoleAdmin, policy.RoleEditor, policy.RoleEveryone}).
			Count(&stored).Error).To(Succeed())
		Expect(stored).To(BeZero())

		for _, subject := range []models.Person{system, person} {
			ctx := DefaultContext.WithUser(&subject)
			for _, check := range [][2]string{{"*", "*"}, {policy.ObjectCatalog, policy.ActionRead}, {policy.ObjectPeople, policy.ActionRead}} {
				Expect(icrbac.CheckContext(ctx, check[0], check[1])).To(BeFalse(), "%s %s:%s", subject.Name, check[0], check[1])
			}
			Expect(icrbac.AccessSummary(ctx)).To(HaveEach(HaveEach(Equal(icrbac.AccessNone))), subject.Name)

			roles, err := dutyRBAC.RolesForUser(subject.ID.String())
			Expect(err).ToNot(HaveOccurred())
			Expect(roles).ToNot(ContainElements(policy.RoleAdmin, policy.RoleEditor))
		}
	})
})
