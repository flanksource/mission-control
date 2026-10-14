package auth

import (
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/auth/accesstoken"
	"github.com/flanksource/incident-commander/db"
)

var viewerRole = models.AccessTokenRoles{{Namespace: "mission-control-builtin", Name: "viewer"}}

var _ = ginkgo.Describe("Access tokens without roles", ginkgo.Ordered, func() {
	var person, tokenPerson, agent *models.Person

	// legacyToken stores a token as tokens were stored before Roles were required.
	legacyToken := func(personID uuid.UUID, createdBy *uuid.UUID) (string, models.AccessToken) {
		token, err := accesstoken.Generate()
		Expect(err).ToNot(HaveOccurred())
		row := models.AccessToken{Name: "legacy-" + uuid.NewString()[:8], Value: token.Hash(), PersonID: personID, CreatedBy: createdBy}
		Expect(DefaultContext.DB().Create(&row).Error).To(Succeed())
		return token.V2().PlainText(), row
	}

	exists := func(token models.AccessToken) bool {
		var count int64
		Expect(DefaultContext.DB().Model(&models.AccessToken{}).Where("id = ?", token.ID).Count(&count).Error).To(Succeed())
		return count == 1
	}

	ginkgo.BeforeAll(func() {
		var err error
		person, err = db.CreatePerson(DefaultContext, "Legacy Owner", "legacy-"+uuid.NewString()[:8]+"@example.com", "")
		Expect(err).ToNot(HaveOccurred())
		tokenPerson, err = db.CreatePerson(DefaultContext, "Legacy Owner (Token)", "legacy-"+uuid.NewString()[:8]+"@example.com", db.PersonTypeAccessToken)
		Expect(err).ToNot(HaveOccurred())
		agent, err = db.CreatePerson(DefaultContext, "agent-"+uuid.NewString()[:8], uuid.NewString()[:8]+"@local", db.PersonTypeAgent)
		Expect(err).ToNot(HaveOccurred())
	})

	ginkgo.It("can't be created for a person", func() {
		_, _, err := db.CreateAccessToken(DefaultContext, person.ID, "no-roles", nil, nil, false, nil)
		Expect(err).To(MatchError(ContainSubstring("must list at least one role")))
	})

	ginkgo.It("are the only tokens an agent can have", func() {
		_, _, err := db.CreateAccessToken(DefaultContext, agent.ID, "default", nil, nil, false, viewerRole)
		Expect(err).To(MatchError(ContainSubstring("can't list roles")))

		token, _, err := db.CreateAccessToken(DefaultContext, agent.ID, "default", nil, nil, false, nil)
		Expect(err).ToNot(HaveOccurred())
		accessToken, err := getAccessToken(DefaultContext, token.PlainText())
		Expect(err).ToNot(HaveOccurred())
		Expect(accessToken.PersonID).To(Equal(agent.ID))
	})

	ginkgo.It("don't authenticate a person", func() {
		for _, owner := range []*models.Person{person, tokenPerson} {
			token, _ := legacyToken(owner.ID, &person.ID)
			_, err := getAccessToken(DefaultContext, token)
			Expect(err).To(MatchError(errTokenWithoutRoles))
		}
	})

	ginkgo.It("are deleted on startup, except agents'", func() {
		_, own := legacyToken(person.ID, nil)
		_, delegated := legacyToken(tokenPerson.ID, &person.ID)
		_, agentToken, err := db.CreateAccessToken(DefaultContext, agent.ID, "agent-cleanup", nil, nil, false, nil)
		Expect(err).ToNot(HaveOccurred())
		_, withRoles, err := db.CreateAccessToken(DefaultContext, person.ID, "with-roles", nil, &person.ID, false, viewerRole)
		Expect(err).ToNot(HaveOccurred())

		Expect(DeleteTokensWithoutRoles(DefaultContext)).To(Succeed())

		Expect(exists(own)).To(BeFalse())
		Expect(exists(delegated)).To(BeFalse())
		Expect(exists(*agentToken)).To(BeTrue())
		Expect(exists(*withRoles)).To(BeTrue())
	})
})
