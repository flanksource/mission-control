package auth

import (
	"os"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
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
		guestUserDirectPerms *models.Person
		adminUser            *models.Person

		directPlaybookPermission  *models.Permission
		directCanaryPermission    *models.Permission
		directComponentPermission *models.Permission
	)

	BeforeAll(func() {
		// Initialize RBAC
		err := rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)
		Expect(err).ToNot(HaveOccurred())

		guestUser = setup.CreateUserWithRole(DefaultContext, "Guest User", "guest@test.com", policy.RoleGuest)
		guestUserNoPerms = setup.CreateUserWithRole(DefaultContext, "Guest User No Permissions", "guest-noperms@test.com", policy.RoleGuest)
		guestUserDirectPerms = setup.CreateUserWithRole(DefaultContext, "Guest User Direct Permissions", "guest-direct@test.com", policy.RoleGuest)
		adminUser = setup.CreateUserWithRole(DefaultContext, "Admin User", "admin@test.com", policy.RoleAdmin)

		// Load fixtures
		loadScopes()
		loadPermissions()

		// Create direct ID-based permissions for guest user with direct permissions
		// These test permissions that use specific resource IDs instead of object_selector
		directPlaybookPermission = &models.Permission{
			ID:          uuid.New(),
			Name:        "direct-playbook-permission",
			Namespace:   "default",
			Action:      policy.ActionRead,
			Subject:     guestUserDirectPerms.ID.String(),
			SubjectType: models.PermissionSubjectTypePerson,
			PlaybookID:  &dummy.EchoConfig.ID,
		}
		err = DefaultContext.DB().Create(directPlaybookPermission).Error
		Expect(err).ToNot(HaveOccurred())

		directCanaryPermission = &models.Permission{
			ID:          uuid.New(),
			Name:        "direct-canary-permission",
			Namespace:   "default",
			Action:      policy.ActionRead,
			Subject:     guestUserDirectPerms.ID.String(),
			SubjectType: models.PermissionSubjectTypePerson,
			CanaryID:    &dummy.LogisticsAPICanary.ID,
		}
		err = DefaultContext.DB().Create(directCanaryPermission).Error
		Expect(err).ToNot(HaveOccurred())

		directComponentPermission = &models.Permission{
			ID:          uuid.New(),
			Name:        "direct-component-permission",
			Namespace:   "default",
			Action:      policy.ActionRead,
			Subject:     guestUserDirectPerms.ID.String(),
			SubjectType: models.PermissionSubjectTypePerson,
			ComponentID: &dummy.Logistics.ID,
		}
		err = DefaultContext.DB().Create(directComponentPermission).Error
		Expect(err).ToNot(HaveOccurred())
	})

	AfterAll(func() {
		// Clean up direct permissions
		permissions := []*models.Permission{directPlaybookPermission, directCanaryPermission, directComponentPermission}
		for _, perm := range permissions {
			if perm != nil {
				err := DefaultContext.DB().Delete(perm).Error
				Expect(err).ToNot(HaveOccurred())
			}
		}

		// Clean up users
		users := []*models.Person{guestUser, guestUserNoPerms, guestUserDirectPerms, adminUser}
		for _, user := range users {
			if user != nil {
				err := DefaultContext.DB().Delete(user).Error
				Expect(err).ToNot(HaveOccurred())
			}
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

	It("should include direct ID-based permissions in RLS payload", func() {
		ctx := DefaultContext.WithUser(guestUserDirectPerms)

		payload, err := GetRLSPayload(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(payload).ToNot(BeNil())

		Expect(payload.Disable).To(BeFalse(), "RLS should be enabled for guest users")

		// Verify playbook scope includes the direct playbook ID
		var hasPlaybookID bool
		for _, scope := range payload.Playbook {
			if scope.ID == dummy.EchoConfig.ID.String() {
				hasPlaybookID = true
				break
			}
		}
		Expect(hasPlaybookID).To(BeTrue(), "playbook scope should include direct playbook ID")

		// Verify canary scope includes the direct canary ID
		var hasCanaryID bool
		for _, scope := range payload.Canary {
			if scope.ID == dummy.LogisticsAPICanary.ID.String() {
				hasCanaryID = true
				break
			}
		}
		Expect(hasCanaryID).To(BeTrue(), "canary scope should include direct canary ID")

		// Verify component scope includes the direct component ID
		var hasComponentID bool
		for _, scope := range payload.Component {
			if scope.ID == dummy.Logistics.ID.String() {
				hasComponentID = true
				break
			}
		}
		Expect(hasComponentID).To(BeTrue(), "component scope should include direct component ID")
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
