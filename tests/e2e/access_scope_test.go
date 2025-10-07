package e2e

import (
	"fmt"

	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	k8sTypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
)

var _ = ginkgo.Describe("AccessScope Integration", ginkgo.Ordered, func() {
	var (
		guestUser    models.Person
		adminUser    models.Person
		guestTeam    models.Team
		teamMember   models.Person
		accessScope1 *v1.AccessScope
		accessScope2 *v1.AccessScope
		teamScope    *v1.AccessScope
	)

	ginkgo.BeforeAll(func() {
		// Create guest user
		guestUser = models.Person{
			ID:    uuid.New(),
			Name:  "Guest User",
			Email: "guest@example.com",
		}
		err := DefaultContext.DB().Create(&guestUser).Error
		Expect(err).To(BeNil())

		err = dutyRBAC.AddRoleForUser(guestUser.ID.String(), policy.RoleGuest)
		Expect(err).To(BeNil())

		// Create admin user
		adminUser = models.Person{
			ID:    uuid.New(),
			Name:  "Admin User",
			Email: "admin@example.com",
		}
		err = DefaultContext.DB().Create(&adminUser).Error
		Expect(err).To(BeNil())

		err = dutyRBAC.AddRoleForUser(adminUser.ID.String(), policy.RoleAdmin)
		Expect(err).To(BeNil())

		// Create team and team member
		guestTeam = models.Team{
			ID:        uuid.New(),
			Name:      "Guest Team",
			CreatedBy: guestUser.ID, // Set created_by to satisfy foreign key constraint
			Source:    models.SourceUI,
		}
		err = DefaultContext.DB().Create(&guestTeam).Error
		Expect(err).To(BeNil())

		teamMember = models.Person{
			ID:    uuid.New(),
			Name:  "Team Member",
			Email: "teammember@example.com",
		}
		err = DefaultContext.DB().Create(&teamMember).Error
		Expect(err).To(BeNil())

		err = dutyRBAC.AddRoleForUser(teamMember.ID.String(), policy.RoleGuest)
		Expect(err).To(BeNil())

		// Add team member to team (use raw SQL since there's no TeamMember model)
		err = DefaultContext.DB().Exec("INSERT INTO team_members (team_id, person_id) VALUES (?, ?)", guestTeam.ID, teamMember.ID).Error
		Expect(err).To(BeNil())
	})

	ginkgo.AfterAll(func() {
		// Cleanup
		DefaultContext.DB().Exec("DELETE FROM team_members WHERE person_id = ?", teamMember.ID)
		DefaultContext.DB().Delete(&guestTeam)

		_, err := dutyRBAC.Enforcer().DeleteRolesForUser(guestUser.ID.String())
		Expect(err).To(BeNil())
		_, err = dutyRBAC.Enforcer().DeleteRolesForUser(adminUser.ID.String())
		Expect(err).To(BeNil())
		_, err = dutyRBAC.Enforcer().DeleteRolesForUser(teamMember.ID.String())
		Expect(err).To(BeNil())

		DefaultContext.DB().Delete(&guestUser)
		DefaultContext.DB().Delete(&adminUser)
		DefaultContext.DB().Delete(&teamMember)

		if accessScope1 != nil {
			DefaultContext.DB().Where("name = ? AND namespace = ?", accessScope1.Name, accessScope1.Namespace).Delete(&models.AccessScope{})
		}
		if accessScope2 != nil {
			DefaultContext.DB().Where("name = ? AND namespace = ?", accessScope2.Name, accessScope2.Namespace).Delete(&models.AccessScope{})
		}
		if teamScope != nil {
			DefaultContext.DB().Where("name = ? AND namespace = ?", teamScope.Name, teamScope.Namespace).Delete(&models.AccessScope{})
		}
	})

	ginkgo.Context("Guest user with AccessScope", func() {
		ginkgo.It("should see only resources within AccessScope (namespace filter)", func() {
			// Create AccessScope for missioncontrol namespace
			yamlData := []byte(`
apiVersion: mission-control.flanksource.com/v1
kind: AccessScope
metadata:
  name: guest-missioncontrol-access
  namespace: default
spec:
  description: Guest user can see resources in missioncontrol namespace
  subject:
    person: guest@example.com
  resources: ["*"]
  scopes:
    - tags:
        namespace: missioncontrol
`)
			err := yaml.Unmarshal(yamlData, &accessScope1)
			Expect(err).To(BeNil())

			accessScope1.UID = k8sTypes.UID(uuid.New().String())
			err = db.PersistAccessScopeFromCRD(DefaultContext, accessScope1)
			Expect(err).To(BeNil())

			// Flush cache to ensure fresh RLS payload
			auth.FlushTokenCache()

			// Get RLS payload for guest user
			ctx := DefaultContext.WithUser(&guestUser)
			rlsPayload, err := auth.GetRLSPayload(ctx)
			Expect(err).To(BeNil())
			Expect(rlsPayload.Disable).To(BeFalse())
			Expect(rlsPayload.Config).To(HaveLen(1))
			Expect(rlsPayload.Config[0].Tags).To(HaveKeyWithValue("namespace", "missioncontrol"))

			// Query configs directly from database with RLS enabled
			var visibleConfigs []models.ConfigItem
			err = ctx.DB().Raw(`
				SELECT * FROM config_items
				WHERE deleted_at IS NULL
			`).Scan(&visibleConfigs).Error
			Expect(err).To(BeNil())

			// Should only see configs in missioncontrol namespace
			for _, config := range visibleConfigs {
				Expect(config.Tags).To(HaveKeyWithValue("namespace", "missioncontrol"), fmt.Sprintf("Config %s outside allowed namespace was visible", lo.FromPtr(config.Name)))
			}

			// Verify specific configs are visible
			configIDs := []uuid.UUID{}
			for _, c := range visibleConfigs {
				configIDs = append(configIDs, c.ID)
			}

			// LogisticsAPIPodConfig is in missioncontrol namespace, should be visible
			Expect(configIDs).To(ContainElement(dummy.LogisticsAPIPodConfig.ID))

			// NginxIngressPod is in ingress-nginx namespace, should NOT be visible
			Expect(configIDs).ToNot(ContainElement(dummy.NginxIngressPod.ID))
		})

		ginkgo.It("should not see any resources without AccessScope", func() {
			// Delete the AccessScope
			err := DefaultContext.DB().Where("name = ?", accessScope1.Name).Delete(&models.AccessScope{}).Error
			Expect(err).To(BeNil())

			// Flush cache
			auth.FlushTokenCache()

			// Get RLS payload - should have empty filters
			ctx := DefaultContext.WithUser(&guestUser)
			rlsPayload, err := auth.GetRLSPayload(ctx)
			Expect(err).To(BeNil())
			Expect(rlsPayload.Disable).To(BeFalse())
			Expect(rlsPayload.Config).To(BeEmpty())

			// Query configs - should see nothing (RLS will filter all)
			var visibleConfigs []models.ConfigItem
			err = ctx.DB().Raw(`
				SELECT * FROM config_items
				WHERE deleted_at IS NULL
			`).Scan(&visibleConfigs).Error
			Expect(err).To(BeNil())

			// With no AccessScope, guest user should see no configs
			Expect(visibleConfigs).To(BeEmpty())
		})

		ginkgo.It("should combine multiple AccessScopes with OR logic", func() {
			// Create two AccessScopes with different namespace filters
			yamlData1 := []byte(`
apiVersion: mission-control.flanksource.com/v1
kind: AccessScope
metadata:
  name: guest-missioncontrol-access
  namespace: default
spec:
  description: Access to missioncontrol namespace
  subject:
    person: guest@example.com
  resources: ["*"]
  scopes:
    - tags:
        namespace: missioncontrol
`)
			err := yaml.Unmarshal(yamlData1, &accessScope1)
			Expect(err).To(BeNil())
			accessScope1.UID = k8sTypes.UID(uuid.New().String())
			err = db.PersistAccessScopeFromCRD(DefaultContext, accessScope1)
			Expect(err).To(BeNil())

			yamlData2 := []byte(`
apiVersion: mission-control.flanksource.com/v1
kind: AccessScope
metadata:
  name: guest-ingress-access
  namespace: default
spec:
  description: Access to ingress-nginx namespace
  subject:
    person: guest@example.com
  resources: ["*"]
  scopes:
    - tags:
        namespace: ingress-nginx
`)
			err = yaml.Unmarshal(yamlData2, &accessScope2)
			Expect(err).To(BeNil())
			accessScope2.UID = k8sTypes.UID(uuid.New().String())
			err = db.PersistAccessScopeFromCRD(DefaultContext, accessScope2)
			Expect(err).To(BeNil())

			// Flush cache
			auth.FlushTokenCache()

			// Get RLS payload - should have both namespace filters
			ctx := DefaultContext.WithUser(&guestUser)
			rlsPayload, err := auth.GetRLSPayload(ctx)
			Expect(err).To(BeNil())
			Expect(rlsPayload.Disable).To(BeFalse())
			Expect(rlsPayload.Config).To(HaveLen(2))

			// Query AccessScopes directly
			scopes, err := mcRBAC.GetAccessScopesForPerson(ctx, guestUser.ID)
			Expect(err).To(BeNil())
			Expect(scopes).To(HaveLen(2))

			// Query configs
			var visibleConfigs []models.ConfigItem
			err = ctx.DB().Raw(`
				SELECT * FROM config_items
				WHERE deleted_at IS NULL
			`).Scan(&visibleConfigs).Error
			Expect(err).To(BeNil())

			configIDs := []uuid.UUID{}
			for _, c := range visibleConfigs {
				configIDs = append(configIDs, c.ID)
			}

			// Should see configs from both namespaces
			Expect(configIDs).To(ContainElement(dummy.LogisticsAPIPodConfig.ID)) // missioncontrol
			Expect(configIDs).To(ContainElement(dummy.NginxIngressPod.ID))       // ingress-nginx

			// Cleanup
			DefaultContext.DB().Where("name IN ?", []string{accessScope1.Name, accessScope2.Name}).Delete(&models.AccessScope{})
		})

		ginkgo.It("should use AND logic within a single scope", func() {
			// Create AccessScope with multiple criteria in one scope (AND logic)
			yamlData := []byte(`
apiVersion: mission-control.flanksource.com/v1
kind: AccessScope
metadata:
  name: guest-multi-criteria
  namespace: default
spec:
  description: Access with multiple AND criteria
  subject:
    person: guest@example.com
  resources: ["*"]
  scopes:
    - tags:
        namespace: missioncontrol
        cluster: demo
`)
			err := yaml.Unmarshal(yamlData, &accessScope1)
			Expect(err).To(BeNil())
			accessScope1.UID = k8sTypes.UID(uuid.New().String())
			err = db.PersistAccessScopeFromCRD(DefaultContext, accessScope1)
			Expect(err).To(BeNil())

			// Flush cache
			auth.FlushTokenCache()

			// Get RLS payload
			ctx := DefaultContext.WithUser(&guestUser)
			rlsPayload, err := auth.GetRLSPayload(ctx)
			Expect(err).To(BeNil())
			Expect(rlsPayload.Disable).To(BeFalse())
			Expect(rlsPayload.Config).To(HaveLen(1))
			Expect(rlsPayload.Config[0].Tags).To(HaveKeyWithValue("namespace", "missioncontrol"))
			Expect(rlsPayload.Config[0].Tags).To(HaveKeyWithValue("cluster", "demo"))

			// Query configs - should only see those matching BOTH tags
			var visibleConfigs []models.ConfigItem
			err = ctx.DB().Raw(`
				SELECT * FROM config_items
				WHERE deleted_at IS NULL
			`).Scan(&visibleConfigs).Error
			Expect(err).To(BeNil())

			for _, config := range visibleConfigs {
				Expect(config.Tags).To(HaveKeyWithValue("namespace", "missioncontrol"))
				Expect(config.Tags).To(HaveKeyWithValue("cluster", "demo"))
			}

			// Cleanup
			DefaultContext.DB().Where("name = ?", accessScope1.Name).Delete(&models.AccessScope{})
		})

		ginkgo.It("should inherit team AccessScopes", func() {
			// Create AccessScope for the team
			yamlData := []byte(`
apiVersion: mission-control.flanksource.com/v1
kind: AccessScope
metadata:
  name: team-missioncontrol-access
  namespace: default
spec:
  description: Team access to missioncontrol namespace
  subject:
    team: Guest Team
  resources: ["*"]
  scopes:
    - tags:
        namespace: missioncontrol
`)
			err := yaml.Unmarshal(yamlData, &teamScope)
			Expect(err).To(BeNil())
			teamScope.UID = k8sTypes.UID(uuid.New().String())
			err = db.PersistAccessScopeFromCRD(DefaultContext, teamScope)
			Expect(err).To(BeNil())

			// Flush cache
			auth.FlushTokenCache()

			// Team member should inherit team's AccessScope
			ctx := DefaultContext.WithUser(&teamMember)
			scopes, err := mcRBAC.GetAccessScopesForPerson(ctx, teamMember.ID)
			Expect(err).To(BeNil())
			Expect(scopes).To(HaveLen(1))

			// Verify RLS payload includes team scope
			rlsPayload, err := auth.GetRLSPayload(ctx)
			Expect(err).To(BeNil())
			Expect(rlsPayload.Disable).To(BeFalse())
			Expect(rlsPayload.Config).To(HaveLen(1))
			Expect(rlsPayload.Config[0].Tags).To(HaveKeyWithValue("namespace", "missioncontrol"))

			// Query configs as team member
			var visibleConfigs []models.ConfigItem
			err = ctx.DB().Raw(`
				SELECT * FROM config_items
				WHERE deleted_at IS NULL
			`).Scan(&visibleConfigs).Error
			Expect(err).To(BeNil())

			configIDs := []uuid.UUID{}
			for _, c := range visibleConfigs {
				configIDs = append(configIDs, c.ID)
			}

			// Should see configs in missioncontrol namespace via team membership
			Expect(configIDs).To(ContainElement(dummy.LogisticsAPIPodConfig.ID))

			// Cleanup
			DefaultContext.DB().Where("name = ?", teamScope.Name).Delete(&models.AccessScope{})
		})

		ginkgo.It("should filter by agent IDs", func() {
			// First, ensure GCPAgent exists and link a config to it
			err := DefaultContext.DB().Save(&dummy.GCPAgent).Error
			Expect(err).To(BeNil())

			// Update a config to have the GCP agent
			err = DefaultContext.DB().Model(&models.ConfigItem{}).
				Where("id = ?", dummy.KubernetesNodeA.ID).
				Update("agent_id", dummy.GCPAgent.ID).Error
			Expect(err).To(BeNil())

			// Create AccessScope with agent filter
			yamlData := []byte(`
apiVersion: mission-control.flanksource.com/v1
kind: AccessScope
metadata:
  name: guest-agent-access
  namespace: default
spec:
  description: Access to configs from GCP agent
  subject:
    person: guest@example.com
  resources: ["*"]
  scopes:
    - agents:
        - ` + dummy.GCPAgent.ID.String() + `
`)
			err = yaml.Unmarshal(yamlData, &accessScope1)
			Expect(err).To(BeNil())
			accessScope1.UID = k8sTypes.UID(uuid.New().String())
			err = db.PersistAccessScopeFromCRD(DefaultContext, accessScope1)
			Expect(err).To(BeNil())

			// Flush cache
			auth.FlushTokenCache()

			// Get RLS payload
			ctx := DefaultContext.WithUser(&guestUser)
			rlsPayload, err := auth.GetRLSPayload(ctx)
			Expect(err).To(BeNil())
			Expect(rlsPayload.Disable).To(BeFalse())
			Expect(rlsPayload.Config[0].Agents).To(ContainElement(dummy.GCPAgent.ID.String()))

			// Cleanup
			DefaultContext.DB().Where("name = ?", accessScope1.Name).Delete(&models.AccessScope{})
			DefaultContext.DB().Model(&models.ConfigItem{}).
				Where("id = ?", dummy.KubernetesNodeA.ID).
				Update("agent_id", nil)
		})
	})

	ginkgo.Context("Admin user", func() {
		ginkgo.It("should see all resources regardless of AccessScope", func() {
			// Create AccessScope for guest (should not affect admin)
			yamlData := []byte(`
apiVersion: mission-control.flanksource.com/v1
kind: AccessScope
metadata:
  name: guest-restricted-access
  namespace: default
spec:
  description: Restricted access for guest
  subject:
    person: guest@example.com
  resources: ["*"]
  scopes:
    - tags:
        namespace: missioncontrol
`)
			err := yaml.Unmarshal(yamlData, &accessScope1)
			Expect(err).To(BeNil())
			accessScope1.UID = k8sTypes.UID(uuid.New().String())
			err = db.PersistAccessScopeFromCRD(DefaultContext, accessScope1)
			Expect(err).To(BeNil())

			// Flush cache
			auth.FlushTokenCache()

			// Admin should have RLS disabled
			ctx := DefaultContext.WithUser(&adminUser)
			rlsPayload, err := auth.GetRLSPayload(ctx)
			Expect(err).To(BeNil())
			Expect(rlsPayload.Disable).To(BeTrue())

			// Query all configs as admin
			var allConfigs []models.ConfigItem
			err = ctx.DB().Raw(`
				SELECT * FROM config_items
				WHERE deleted_at IS NULL
			`).Scan(&allConfigs).Error
			Expect(err).To(BeNil())

			// Admin should see all configs
			Expect(len(allConfigs)).To(BeNumerically(">", 0))

			// Cleanup
			DefaultContext.DB().Where("name = ?", accessScope1.Name).Delete(&models.AccessScope{})
		})
	})
})
