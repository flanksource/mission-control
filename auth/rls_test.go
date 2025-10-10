package auth

import (
	"os"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = Describe("GetRLSPayload", Ordered, func() {
	var (
		guestUser            *models.Person
		guestUserNoPerms     *models.Person
		adminUser            *models.Person
	)

	BeforeAll(func() {
		// Initialize RBAC
		err := rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)
		Expect(err).ToNot(HaveOccurred())

		// Create a guest user
		guestUser = &models.Person{
			ID:    uuid.New(),
			Name:  "Guest User",
			Email: "guest@test.com",
		}
		err = DefaultContext.DB().Create(guestUser).Error
		Expect(err).ToNot(HaveOccurred())

		// Assign guest role to the user
		_, err = rbac.Enforcer().AddRoleForUser(guestUser.ID.String(), policy.RoleGuest)
		Expect(err).ToNot(HaveOccurred())

		// Create a guest user with no permissions
		guestUserNoPerms = &models.Person{
			ID:    uuid.New(),
			Name:  "Guest User No Permissions",
			Email: "guest-noperms@test.com",
		}
		err = DefaultContext.DB().Create(guestUserNoPerms).Error
		Expect(err).ToNot(HaveOccurred())

		// Assign guest role but no permissions
		_, err = rbac.Enforcer().AddRoleForUser(guestUserNoPerms.ID.String(), policy.RoleGuest)
		Expect(err).ToNot(HaveOccurred())

		// Create an admin user
		adminUser = &models.Person{
			ID:    uuid.New(),
			Name:  "Admin User",
			Email: "admin@test.com",
		}
		err = DefaultContext.DB().Create(adminUser).Error
		Expect(err).ToNot(HaveOccurred())

		// Assign admin role
		_, err = rbac.Enforcer().AddRoleForUser(adminUser.ID.String(), policy.RoleAdmin)
		Expect(err).ToNot(HaveOccurred())

		// Load fixtures
		loadScopes()
		loadPermissions()
	})

	AfterAll(func() {
		// Clean up the guest user
		if guestUser != nil {
			err := DefaultContext.DB().Delete(guestUser).Error
			Expect(err).ToNot(HaveOccurred())
		}

		// Clean up the guest user with no permissions
		if guestUserNoPerms != nil {
			err := DefaultContext.DB().Delete(guestUserNoPerms).Error
			Expect(err).ToNot(HaveOccurred())
		}

		// Clean up the admin user
		if adminUser != nil {
			err := DefaultContext.DB().Delete(adminUser).Error
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("should return RLS payload with config scope for guest user", func() {
		ctx := DefaultContext.WithUser(guestUser)

		payload, err := GetRLSPayload(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(payload).ToNot(BeNil())

		Expect(payload.Disable).To(BeFalse(), "RLS should be enabled for guest users with scopes")
		Expect(payload.Config).To(HaveLen(3), "should have three config scopes")
		Expect(payload.Config).To(ContainElements([]rls.Scope{
			{Tags: map[string]string{"namespace": "missioncontrol"}},
			{Tags: map[string]string{"namespace": "monitoring"}},
			{Tags: map[string]string{"namespace": "media"}},
		}))

		// Other resource types should be empty since we only granted config access
		Expect(payload.Component).To(BeEmpty(), "component scope should be empty")
		Expect(payload.Playbook).To(BeEmpty(), "playbook scope should be empty")
		Expect(payload.Canary).To(BeEmpty(), "canary scope should be empty")
	})

	It("should disable RLS for non-guest users", func() {
		ctx := DefaultContext.WithUser(adminUser)

		payload, err := GetRLSPayload(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(payload).ToNot(BeNil())

		// Admin users should have RLS disabled
		Expect(payload.Disable).To(BeTrue(), "RLS should be disabled for admin users")
	})

	It("should return empty RLS payload for guest user with no permissions", func() {
		ctx := DefaultContext.WithUser(guestUserNoPerms)

		payload, err := GetRLSPayload(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(payload).ToNot(BeNil())

		// RLS should be enabled (not disabled) for guest users even without permissions
		Expect(payload.Disable).To(BeFalse(), "RLS should be enabled for guest users even without permissions")

		// All resource scopes should be empty since user has no permissions
		Expect(payload.Config).To(BeEmpty(), "config scope should be empty for guest user with no permissions")
		Expect(payload.Component).To(BeEmpty(), "component scope should be empty for guest user with no permissions")
		Expect(payload.Playbook).To(BeEmpty(), "playbook scope should be empty for guest user with no permissions")
		Expect(payload.Canary).To(BeEmpty(), "canary scope should be empty for guest user with no permissions")
	})
})

func loadScopes() {
	scopeFiles, err := os.ReadDir("testdata/scopes")
	Expect(err).ToNot(HaveOccurred())

	for _, file := range scopeFiles {
		if file.IsDir() {
			continue
		}

		scopeBytes, err := os.ReadFile("testdata/scopes/" + file.Name())
		Expect(err).ToNot(HaveOccurred())

		var scope v1.Scope
		err = yaml.Unmarshal(scopeBytes, &scope)
		Expect(err).ToNot(HaveOccurred())

		err = db.PersistScopeFromCRD(DefaultContext, &scope)
		Expect(err).ToNot(HaveOccurred())
	}
}

func loadPermissions() {
	permFiles, err := os.ReadDir("testdata/permissions")
	Expect(err).ToNot(HaveOccurred())

	for _, file := range permFiles {
		if file.IsDir() {
			continue
		}

		permBytes, err := os.ReadFile("testdata/permissions/" + file.Name())
		Expect(err).ToNot(HaveOccurred())

		var permission v1.Permission
		err = yaml.Unmarshal(permBytes, &permission)
		Expect(err).ToNot(HaveOccurred())

		err = db.PersistPermissionFromCRD(DefaultContext, &permission)
		Expect(err).ToNot(HaveOccurred())
	}
}
