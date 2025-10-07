package rbac

import (
	"encoding/json"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/lib/pq"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = Describe("AccessScope Queries", func() {
	var ctx context.Context
	var person models.Person
	var team models.Team
	var scope1, scope2 models.AccessScope

	BeforeEach(func() {
		ctx = DefaultContext

		// Use existing dummy fixtures
		person = dummy.JohnDoe
		team = dummy.BackendTeam

		// Ensure GCPAgent exists
		Expect(ctx.DB().Save(&dummy.GCPAgent).Error).ToNot(HaveOccurred())

		// Add person to team if not already a member
		var count int64
		ctx.DB().Table("team_members").Where("team_id = ? AND person_id = ?", team.ID, person.ID).Count(&count)
		if count == 0 {
			// Insert directly into team_members table
			ctx.DB().Exec("INSERT INTO team_members (team_id, person_id) VALUES (?, ?)", team.ID, person.ID)
		}

		// Create AccessScopes
		scopes1JSON, _ := json.Marshal([]AccessScopeScope{
			{Tags: map[string]string{"namespace": "test"}},
		})
		scope1 = models.AccessScope{
			ID:        uuid.New(),
			Name:      "test-person-scope",
			Namespace: "default",
			PersonID:  &person.ID,
			Resources: pq.StringArray{string(v1.AccessScopeResourceConfig)},
			Scopes:    types.JSON(scopes1JSON),
			Source:    models.SourceCRD,
		}
		Expect(ctx.DB().Create(&scope1).Error).ToNot(HaveOccurred())

		scopes2JSON, _ := json.Marshal([]AccessScopeScope{
			{Agents: []string{dummy.GCPAgent.Name}},
		})
		scope2 = models.AccessScope{
			ID:        uuid.New(),
			Name:      "test-team-scope",
			Namespace: "default",
			TeamID:    &team.ID,
			Resources: pq.StringArray{string(v1.AccessScopeResourceComponent)},
			Scopes:    types.JSON(scopes2JSON),
			Source:    models.SourceCRD,
		}
		Expect(ctx.DB().Create(&scope2).Error).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		// Clean up test data
		ctx.DB().Unscoped().Delete(&scope1)
		ctx.DB().Unscoped().Delete(&scope2)
	})

	It("should get AccessScopes for person including team scopes", func() {
		scopes, err := GetAccessScopesForPerson(ctx, person.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(scopes).To(HaveLen(2))

		// Should include both person and team scopes
		ids := []uuid.UUID{scopes[0].ID, scopes[1].ID}
		Expect(ids).To(ContainElements(scope1.ID, scope2.ID))
	})

	It("should build RLS payload from scopes", func() {
		scopes := []models.AccessScope{scope1, scope2}
		payload, err := GetRLSPayloadFromAccessScopes(ctx, scopes)
		Expect(err).ToNot(HaveOccurred())

		Expect(payload.Config).To(HaveLen(1))
		Expect(payload.Config[0].Tags).To(HaveKeyWithValue("namespace", "test"))

		Expect(payload.Component).To(HaveLen(1))
		Expect(payload.Component[0].Agents).To(ConsistOf(dummy.GCPAgent.ID.String()))
	})

	It("should expand wildcard resource to all resource types", func() {
		scopesJSON, _ := json.Marshal([]AccessScopeScope{
			{Tags: map[string]string{"cluster": "homelab"}},
		})
		wildcardScope := models.AccessScope{
			ID:        uuid.New(),
			Name:      "wildcard-scope",
			Namespace: "default",
			PersonID:  &person.ID,
			Resources: pq.StringArray{string(v1.AccessScopeResourceAll)},
			Scopes:    types.JSON(scopesJSON),
			Source:    models.SourceCRD,
		}

		scopes := []models.AccessScope{wildcardScope}
		payload, err := GetRLSPayloadFromAccessScopes(ctx, scopes)
		Expect(err).ToNot(HaveOccurred())

		// Should have scopes for all four resource types
		Expect(payload.Config).To(HaveLen(1))
		Expect(payload.Config[0].Tags).To(HaveKeyWithValue("cluster", "homelab"))

		Expect(payload.Component).To(HaveLen(1))
		Expect(payload.Component[0].Tags).To(HaveKeyWithValue("cluster", "homelab"))

		Expect(payload.Canary).To(HaveLen(1))
		Expect(payload.Canary[0].Tags).To(HaveKeyWithValue("cluster", "homelab"))

		Expect(payload.Playbook).To(HaveLen(1))
		Expect(payload.Playbook[0].Tags).To(HaveKeyWithValue("cluster", "homelab"))
	})
})
