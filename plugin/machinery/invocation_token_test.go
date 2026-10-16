package machinery

import (
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/auth/signing"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/plugin"
	icrbac "github.com/flanksource/incident-commander/rbac"
)

var _ = ginkgo.Describe("Plugin invocations made with an access token", ginkgo.Ordered, func() {
	var (
		owner      *models.Person
		tokenA     *models.AccessToken
		tokenB     *models.AccessToken
		entry      = &plugin.Entry{ID: uuid.New(), Name: "invocation-token-test"}
		viewerRole = db.AccessTokenRoles{{Namespace: "mission-control-builtin", Name: "viewer"}}
	)

	mint := func(accessToken *uuid.UUID) string {
		ginkgo.GinkgoHelper()
		token, err := plugin.MintInvocationToken(owner.ID.String(), owner.ID.String(), accessToken, entry.ID, 0)
		Expect(err).ToNot(HaveOccurred())
		return token
	}

	ginkgo.BeforeAll(func() {
		Expect(icrbac.Init(DefaultContext)).To(Succeed())
		_, _, err := signing.Initialize("/tmp/dummy")
		Expect(err).ToNot(HaveOccurred())

		owner, err = db.CreatePerson(DefaultContext, "Invocation Owner", "invocation-"+uuid.NewString()[:8]+"@example.com", "")
		Expect(err).ToNot(HaveOccurred())
		_, tokenA, err = db.CreateAccessTokenForPerson(DefaultContext, owner, "a", 0, false, viewerRole)
		Expect(err).ToNot(HaveOccurred())
		_, tokenB, err = db.CreateAccessTokenForPerson(DefaultContext, owner, "b", 0, false, viewerRole)
		Expect(err).ToNot(HaveOccurred())
	})

	ginkgo.It("refuses an invocation not minted with the request's access token", func() {
		ctx := icrbac.WithAccessToken(DefaultContext.WithUser(owner), tokenA)
		for _, minted := range []*uuid.UUID{nil, &tokenB.ID} {
			_, err := AuthorizeInvocationToken(ctx, mint(minted), entry, "op", "")
			Expect(err).To(MatchError(ContainSubstring("wasn't minted with this request's access token")))
		}

		// Past that check, the owner simply holds no invoke grant
		_, err := AuthorizeInvocationToken(ctx, mint(&tokenA.ID), entry, "op", "")
		Expect(err).To(MatchError(ContainSubstring("not allowed to invoke")))
	})

	ginkgo.It("limits a callback by the invocation's access token, and refuses it once the token is deleted", func() {
		claims := &plugin.InvocationTokenClaims{AccessToken: &tokenB.ID}
		ctx, err := WithInvocationAccessToken(DefaultContext.WithUser(owner), claims)
		Expect(err).ToNot(HaveOccurred())
		tokenID, limited := icrbac.TokenLimit(ctx, owner.ID.String())
		Expect(limited).To(BeTrue())
		Expect(tokenID).To(Equal(tokenB.ID))

		Expect(db.DeleteAccessToken(DefaultContext, tokenB.ID.String())).To(Succeed())
		_, err = WithInvocationAccessToken(DefaultContext.WithUser(owner), claims)
		Expect(err).To(MatchError(ContainSubstring("was deleted")))
	})
})
