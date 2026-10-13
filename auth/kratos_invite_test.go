package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

var _ = ginkgo.Describe("Kratos invites", ginkgo.Ordered, func() {
	var teams = map[string]models.Team{}
	var invitees []models.Person

	var createdIdentities bool

	// register runs the after-registration webhook for a new identity, registered in Kratos with identityEmail,
	// with a payload that claims payloadEmail
	register := func(identityEmail, payloadEmail string) (models.Person, int) {
		ginkgo.GinkgoHelper()
		person := models.Person{ID: uuid.New(), Name: "Invitee", Email: identityEmail}
		Expect(DefaultContext.DB().Exec(`INSERT INTO identities (id, traits) VALUES (?, ?)`,
			person.ID, `{"email":"`+identityEmail+`"}`).Error).To(Succeed())
		Expect(DefaultContext.DB().Exec(`INSERT INTO people (id, name, email) VALUES (?, ?, ?) ON CONFLICT (id) DO NOTHING`,
			person.ID, person.Name, person.Email).Error).To(Succeed())
		invitees = append(invitees, person)

		body := `{"identity":{"id":"` + person.ID.String() + `","traits":{"email":"` + payloadEmail + `"}}}`
		req := httptest.NewRequest(http.MethodPost, "/auth/kratos/hooks/registration/after", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req = req.WithContext(DefaultContext)
		rec := httptest.NewRecorder()
		Expect((&KratosHandler{}).AfterRegistrationWebhook(echo.New().NewContext(req, rec))).To(Succeed())
		return person, rec.Code
	}

	// accept registers an identity with the invite's email
	accept := func(email string) (models.Person, int) {
		ginkgo.GinkgoHelper()
		return register(email, email)
	}

	roles := func(person models.Person) []string {
		ginkgo.GinkgoHelper()
		roles, err := dutyRBAC.RolesForUser(person.ID.String())
		Expect(err).ToNot(HaveOccurred())
		return roles
	}

	invite := func(email string, grants inviteGrants) {
		ginkgo.GinkgoHelper()
		encoded, err := grants.encode()
		Expect(err).ToNot(HaveOccurred())
		Expect(DefaultContext.DB().Create(&models.Invite{Email: email, Role: encoded}).Error).To(Succeed())
	}

	memberships := func(person models.Person) map[string]string {
		ginkgo.GinkgoHelper()
		var rows []struct{ Name, Source string }
		Expect(DefaultContext.DB().Raw(`SELECT teams.name, team_members.source FROM team_members
			JOIN teams ON teams.id = team_members.team_id WHERE team_members.person_id = ?`, person.ID).
			Scan(&rows).Error).To(Succeed())
		result := map[string]string{}
		for _, r := range rows {
			result[r.Name] = r.Source
		}
		return result
	}

	ginkgo.BeforeAll(func() {
		// Kratos owns the identities table, so the test database doesn't have it
		var exists bool
		Expect(DefaultContext.DB().Raw(`SELECT to_regclass('identities') IS NOT NULL`).Scan(&exists).Error).To(Succeed())
		if !exists {
			Expect(DefaultContext.DB().Exec(`CREATE TABLE identities (id uuid PRIMARY KEY, traits jsonb NOT NULL)`).Error).To(Succeed())
			createdIdentities = true
		}

		creator := models.Person{ID: uuid.New(), Name: "Invite Team Creator"}
		Expect(DefaultContext.DB().Create(&creator).Error).To(Succeed())
		invitees = append(invitees, creator)

		for _, name := range []string{"invite-platform", "invite-sre"} {
			team := models.Team{ID: uuid.New(), Name: name, CreatedBy: creator.ID}
			Expect(DefaultContext.DB().Create(&team).Error).To(Succeed())
			teams[name] = team
		}
	})

	ginkgo.AfterAll(func() {
		ids := make([]uuid.UUID, 0, len(invitees))
		for _, p := range invitees {
			ids = append(ids, p.ID)
		}
		Expect(DefaultContext.DB().Exec("DELETE FROM team_members WHERE person_id IN ?", ids).Error).To(Succeed())
		for _, team := range teams {
			Expect(DefaultContext.DB().Delete(&team).Error).To(Succeed())
		}
		Expect(DefaultContext.DB().Where("email LIKE ?", "invitee-%@example.com").Delete(&models.Invite{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Exec("DELETE FROM identities WHERE id IN ?", ids).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("id IN ?", ids).Delete(&models.Person{}).Error).To(Succeed())
		if createdIdentities {
			Expect(DefaultContext.DB().Exec(`DROP TABLE identities`).Error).To(Succeed())
		}
	})

	ginkgo.It("adds the person to the invite's teams on acceptance, skipping a missing team", func() {
		invite("invitee-teams@example.com", inviteGrants{Teams: []string{"invite-platform", "invite-missing", "invite-sre"}})

		person, code := accept("invitee-teams@example.com")
		Expect(code).To(Equal(http.StatusOK))

		Expect(memberships(person)).To(Equal(map[string]string{
			"invite-platform": SourceInvite,
			"invite-sre":      SourceInvite,
		}))

		Expect(roles(person)).To(ContainElements(teams["invite-platform"].ID.String(), teams["invite-sre"].ID.String()))

		var accepted models.Invite
		Expect(DefaultContext.DB().Where("email = ?", "invitee-teams@example.com").First(&accepted).Error).To(Succeed())
		Expect(accepted.AcceptedAt).ToNot(BeNil())
	})

	ginkgo.It("assigns nothing for the built-in role an earlier version's invite stored", func() {
		for column, teamIDs := range map[string][]string{
			"editor": nil,
			`{"role":"admin","teams":["invite-sre"]}`: {teams["invite-sre"].ID.String()},
		} {
			email := "invitee-role-" + uuid.NewString()[:8] + "@example.com"
			Expect(DefaultContext.DB().Create(&models.Invite{Email: email, Role: column}).Error).To(Succeed())

			person, code := accept(email)
			Expect(code).To(Equal(http.StatusOK))
			Expect(lo.Uniq(roles(person))).To(ConsistOf(teamIDs), column)
		}
	})

	ginkgo.It("assigns a person invited with teams only their teams", func() {
		invite("invitee-guest@example.com", inviteGrants{Teams: []string{"invite-sre"}})

		person, code := accept("invitee-guest@example.com")
		Expect(code).To(Equal(http.StatusOK))
		Expect(roles(person)).To(HaveEach(teams["invite-sre"].ID.String()))
	})

	ginkgo.It("refuses an invite that gives a role", func() {
		_, err := (&KratosHandler{}).createInvite(DefaultContext, InviteUserRequest{Email: "invitee-with-role@example.com", Role: "viewer"})
		Expect(dutyAPI.ErrorCode(err)).To(Equal(dutyAPI.EINVALID))

		var count int64
		Expect(DefaultContext.DB().Model(&models.Invite{}).Where("email = ?", "invitee-with-role@example.com").Count(&count).Error).To(Succeed())
		Expect(count).To(BeZero())
	})

	ginkgo.It("refuses a payload whose identity wasn't registered with the invited email", func() {
		invite("invitee-victim@example.com", inviteGrants{Teams: []string{"invite-platform"}})

		attacker, code := register("invitee-attacker@example.com", "invitee-victim@example.com")
		Expect(code).To(Equal(http.StatusForbidden))
		Expect(memberships(attacker)).To(BeEmpty())
		Expect(roles(attacker)).To(BeEmpty())

		var pending models.Invite
		Expect(DefaultContext.DB().Where("email = ?", "invitee-victim@example.com").First(&pending).Error).To(Succeed())
		Expect(pending.AcceptedAt).To(BeNil())
	})

	ginkgo.It("creates an invite that names only teams", func() {
		created, err := (&KratosHandler{}).createInvite(DefaultContext, InviteUserRequest{
			Email: " Invitee-Created@example.com ",
			Teams: []string{" invite-sre ", "invite-sre", "", "invite-platform"},
		})
		Expect(err).ToNot(HaveOccurred())

		var stored models.Invite
		Expect(DefaultContext.DB().Where("id = ?", created.ID).First(&stored).Error).To(Succeed())
		Expect(stored.Email).To(Equal("invitee-created@example.com"))

		grants, err := decodeInviteGrants(stored.Role)
		Expect(err).ToNot(HaveOccurred())
		Expect(grants).To(Equal(inviteGrants{Teams: []string{"invite-sre", "invite-platform"}}))
	})

	ginkgo.Describe("stored grants", func() {
		ginkgo.It("reads a built-in role an earlier version stored as no grants", func() {
			decoded, err := decodeInviteGrants("viewer")
			Expect(err).ToNot(HaveOccurred())
			Expect(decoded).To(Equal(inviteGrants{}))
		})

		ginkgo.It("round-trips teams", func() {
			grants := inviteGrants{Teams: []string{"a", "b"}}
			encoded, err := grants.encode()
			Expect(err).ToNot(HaveOccurred())

			decoded, err := decodeInviteGrants(encoded)
			Expect(err).ToNot(HaveOccurred())
			Expect(decoded).To(Equal(grants))
		})
	})
})
