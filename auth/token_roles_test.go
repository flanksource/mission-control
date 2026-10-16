package auth

import (
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/db"
)

var viewerRole = db.AccessTokenRoles{{Namespace: "mission-control-builtin", Name: "viewer"}}

var _ = ginkgo.Describe("Access tokens without roles", ginkgo.Ordered, func() {
	var person, agent *models.Person

	ginkgo.BeforeAll(func() {
		var err error
		person, err = db.CreatePerson(DefaultContext, "Legacy Owner", "legacy-"+uuid.NewString()[:8]+"@example.com", "")
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
})
