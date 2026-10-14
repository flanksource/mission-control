package auth

import (
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	client "github.com/ory/client-go"

	dbModels "github.com/flanksource/incident-commander/db/models"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/rbac/shipped"
)

var _ = ginkgo.Describe("Team mapper", ginkgo.Ordered, func() {
	var (
		person models.Person
		teams  = map[string]models.Team{}
	)

	memberships := func() map[string]string {
		var rows []struct {
			Name   string
			Source string
		}
		Expect(DefaultContext.DB().Raw(`SELECT teams.name, team_members.source FROM team_members
			JOIN teams ON teams.id = team_members.team_id WHERE team_members.person_id = ?`, person.ID).
			Scan(&rows).Error).To(Succeed())

		result := map[string]string{}
		for _, r := range rows {
			result[r.Name] = r.Source
		}
		return result
	}

	kratosSession := func(sessionID string, groups ...string) *client.Session {
		return &client.Session{
			Id: sessionID,
			Identity: &client.Identity{
				Id:             person.ID.String(),
				Traits:         map[string]any{"email": person.Email},
				MetadataPublic: map[string]any{"groups": groups},
			},
		}
	}

	ginkgo.BeforeAll(func() {
		person = models.Person{ID: uuid.New(), Name: "Team Mapper", Email: "team-mapper@example.com"}
		Expect(DefaultContext.DB().Create(&person).Error).To(Succeed())

		for _, name := range []string{"mapper-a", "mapper-b", "mapper-direct", "admins"} {
			var team models.Team
			err := DefaultContext.DB().Where("name = ? AND deleted_at IS NULL", name).Find(&team).Error
			Expect(err).ToNot(HaveOccurred())
			if team.ID == uuid.Nil {
				team = models.Team{ID: uuid.New(), Name: name, CreatedBy: person.ID}
				Expect(DefaultContext.DB().Create(&team).Error).To(Succeed())
			}
			teams[name] = team
		}

		ginkgo.DeferCleanup(func() {
			Expect(DefaultContext.DB().Where("person_id = ?", person.ID).Delete(&dbModels.TeamMember{}).Error).To(Succeed())
			_, _ = dutyRBAC.Enforcer().DeleteRolesForUser(person.ID.String())
		})
	})

	ginkgo.BeforeEach(func() {
		teamMapperInputs.Flush()
		IdentityRoleMapper = `{"teams": identity.metadata_public.groups}`
		ginkgo.DeferCleanup(func() { IdentityRoleMapper = "" })
	})

	ginkgo.It("adds and removes the teams the Kratos mapper returns", func() {
		Expect(mapIDsToTeams(DefaultContext, kratosSession("s1", "mapper-a", "mapper-b"), person)).To(Succeed())
		Expect(memberships()).To(Equal(map[string]string{"mapper-a": SourceTeamMapper, "mapper-b": SourceTeamMapper}))

		roles, err := dutyRBAC.Enforcer().GetRolesForUser(person.ID.String())
		Expect(err).ToNot(HaveOccurred())
		Expect(roles).To(ContainElements(teams["mapper-a"].ID.String(), teams["mapper-b"].ID.String()))

		Expect(mapIDsToTeams(DefaultContext, kratosSession("s1", "mapper-b"), person)).To(Succeed())
		Expect(memberships()).To(Equal(map[string]string{"mapper-b": SourceTeamMapper}))

		roles, err = dutyRBAC.Enforcer().GetRolesForUser(person.ID.String())
		Expect(err).ToNot(HaveOccurred())
		Expect(roles).ToNot(ContainElement(teams["mapper-a"].ID.String()))

		Expect(mapIDsToTeams(DefaultContext, kratosSession("s2"), person)).To(Succeed())
		Expect(memberships()).To(BeEmpty())
	})

	ginkgo.It("changes nothing when the mapper returns a role", func() {
		Expect(mapIDsToTeams(DefaultContext, kratosSession("s1", "mapper-a"), person)).To(Succeed())

		for _, expr := range []string{
			`{"role": "admin"}`,
			`{"role": "admin", "teams": ["mapper-b"]}`,
		} {
			teamMapperInputs.Flush()
			IdentityRoleMapper = expr
			Expect(mapIDsToTeams(DefaultContext, kratosSession("s1", "mapper-a"), person)).ToNot(Succeed())
			Expect(memberships()).To(Equal(map[string]string{"mapper-a": SourceTeamMapper}))

			roles, err := dutyRBAC.Enforcer().GetRolesForUser(person.ID.String())
			Expect(err).ToNot(HaveOccurred())
			Expect(roles).ToNot(ContainElement("admin"))
		}

		IdentityRoleMapper = `{"teams": []}`
		Expect(mapIDsToTeams(DefaultContext, kratosSession("s1"), person)).To(Succeed())
		Expect(memberships()).To(BeEmpty())
	})

	ginkgo.It("puts a Clerk org:admin in admins with the default mapper", func() {
		h := &ClerkHandler{}
		h.mapTeams(DefaultContext, person.ID, jwt.MapClaims{"sid": "sess_1", "org_role": "org:admin"})
		Expect(memberships()).To(Equal(map[string]string{"admins": SourceTeamMapper}))

		h.mapTeams(DefaultContext, person.ID, jwt.MapClaims{"sid": "sess_1", "org_role": "org:guest"})
		Expect(memberships()).To(BeEmpty())

		h.mapTeams(DefaultContext, person.ID, jwt.MapClaims{"sid": "sess_1", "o": map[string]any{"id": "org_1", "rol": "admin"}})
		Expect(memberships()).To(Equal(map[string]string{"admins": SourceTeamMapper}))
	})

	ginkgo.It("keeps direct memberships through a sync", func() {
		for _, name := range []string{"mapper-a", "mapper-direct"} {
			Expect(DefaultContext.DB().Create(&dbModels.TeamMember{
				TeamID: teams[name].ID, PersonID: person.ID, Source: models.SourceUI,
			}).Error).To(Succeed())
		}
		ginkgo.DeferCleanup(func() {
			Expect(DefaultContext.DB().Where("person_id = ?", person.ID).Delete(&dbModels.TeamMember{}).Error).To(Succeed())
		})

		Expect(mapIDsToTeams(DefaultContext, kratosSession("s1", "mapper-a", "mapper-b"), person)).To(Succeed())
		Expect(memberships()).To(Equal(map[string]string{
			"mapper-a":      models.SourceUI,
			"mapper-b":      SourceTeamMapper,
			"mapper-direct": models.SourceUI,
		}))

		Expect(mapIDsToTeams(DefaultContext, kratosSession("s1"), person)).To(Succeed())
		Expect(memberships()).To(Equal(map[string]string{
			"mapper-a":      models.SourceUI,
			"mapper-direct": models.SourceUI,
		}))
	})

	ginkgo.It("skips a team that doesn't exist", func() {
		Expect(mapIDsToTeams(DefaultContext, kratosSession("s1", "mapper-a", "mapper-missing"), person)).To(Succeed())
		Expect(memberships()).To(Equal(map[string]string{"mapper-a": SourceTeamMapper}))

		Expect(mapIDsToTeams(DefaultContext, kratosSession("s1"), person)).To(Succeed())
	})

	ginkgo.It("makes a mapper-sourced bootstrap admin membership direct on startup", func() {
		admin, err := GetOrCreateAdminUser(DefaultContext)
		Expect(err).ToNot(HaveOccurred())
		Expect(shipped.Sync(DefaultContext)).To(Succeed())
		Expect(DefaultContext.DB().Exec(`INSERT INTO team_members (team_id, person_id, source)
			SELECT id, ?, ? FROM teams WHERE name = ? AND deleted_at IS NULL
			ON CONFLICT (team_id, person_id) DO UPDATE SET source = EXCLUDED.source`,
			admin.ID, SourceTeamMapper, shipped.TeamAdmins).Error).To(Succeed())

		membership := func() dbModels.TeamMember {
			ginkgo.GinkgoHelper()
			var member dbModels.TeamMember
			Expect(DefaultContext.DB().Table("team_members").
				Joins("JOIN teams ON teams.id = team_members.team_id").
				Where("team_members.person_id = ? AND teams.name = ? AND teams.deleted_at IS NULL", admin.ID, shipped.TeamAdmins).
				First(&member).Error).To(Succeed())
			return member
		}
		Expect(membership().Source).To(Equal(SourceTeamMapper))

		Expect(shipped.AddToAdmins(DefaultContext, admin.ID)).To(Succeed())
		Expect(shipped.Sync(DefaultContext)).To(Succeed())
		Expect(membership().Source).To(Equal(adapter.SourceShipped))

		Expect(mapTeams(DefaultContext, admin.ID, `{"teams": []}`, nil, "bootstrap-admin-without-teams")).To(Succeed())
		Expect(membership().Source).To(Equal(adapter.SourceShipped))
	})
})

var _ = ginkgo.Describe("isAccessTokenRequest", func() {
	request := func(set func(r *http.Request)) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		set(r)
		return r
	}

	ginkgo.It("is true for access tokens, so the Kratos team mapper skips them", func() {
		Expect(isAccessTokenRequest(request(func(r *http.Request) { r.SetBasicAuth("token", "secret") }))).To(BeTrue())
		Expect(isAccessTokenRequest(request(func(r *http.Request) { r.Header.Set("Authorization", "Bearer secret") }))).To(BeTrue())
	})

	ginkgo.It("is false for a Kratos login or session cookie", func() {
		Expect(isAccessTokenRequest(request(func(r *http.Request) { r.SetBasicAuth("alice@example.com", "pw") }))).To(BeFalse())
		Expect(isAccessTokenRequest(request(func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "x"})
		}))).To(BeFalse())
	})
})
