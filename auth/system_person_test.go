package auth

import (
	"fmt"
	"os"

	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/bcrypt"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/vars"
)

var _ = ginkgo.Describe("The System person", func() {
	ginkgo.It("isn't an admin, even where an earlier version stored it as one", func() {
		oldAuthMode, oldOIDCEnabled, oldHtpasswdFile := vars.AuthMode, OIDCEnabled, HtpasswdFile
		oldSystemUserID, oldLocalhostOnly, oldChecker := api.SystemUserID, localhostOnly, checker
		ginkgo.DeferCleanup(func() {
			vars.AuthMode, OIDCEnabled, HtpasswdFile = oldAuthMode, oldOIDCEnabled, oldHtpasswdFile
			api.SystemUserID, localhostOnly, checker = oldSystemUserID, oldLocalhostOnly, oldChecker
		})

		var system models.Person
		Expect(DefaultContext.DB().Where("name = 'System'").First(&system).Error).To(Succeed())
		api.SystemUserID = &system.ID

		storedAdmin := func() int64 {
			ginkgo.GinkgoHelper()
			var count int64
			Expect(DefaultContext.DB().Table("casbin_rule").
				Where("ptype = 'g' AND v0 = ? AND v1 = ?", system.ID.String(), policy.RoleAdmin).
				Count(&count).Error).To(Succeed())
			return count
		}

		// Earlier versions passed the System person to rbac.Init as an admin, which stored the grouping
		_, err := dutyRBAC.Enforcer().AddRoleForUser(system.ID.String(), policy.RoleAdmin)
		Expect(err).ToNot(HaveOccurred())
		Expect(storedAdmin()).To(BeEquivalentTo(1))

		hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		Expect(err).ToNot(HaveOccurred())
		htpasswdPath := ginkgo.GinkgoT().TempDir() + "/htpasswd"
		Expect(os.WriteFile(htpasswdPath, fmt.Appendf(nil, "admin:%s\n", string(hash)), 0600)).To(Succeed())
		vars.AuthMode = Basic
		OIDCEnabled = false
		HtpasswdFile = htpasswdPath

		Expect(Middleware(DefaultContext, newEchoInstance(DefaultContext))).To(Succeed())

		roles, err := dutyRBAC.RolesForUser(system.ID.String())
		Expect(err).ToNot(HaveOccurred())
		Expect(roles).ToNot(ContainElement(policy.RoleAdmin))
		Expect(storedAdmin()).To(BeZero())
	})
})
