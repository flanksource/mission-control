package rbac

import (
	gocontext "context"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("WithRequestIdentity", func() {
	owner := &models.Person{ID: uuid.New(), Name: "owner"}
	token := &models.AccessToken{ID: uuid.New(), PersonID: owner.ID, Roles: models.AccessTokenRoles{{Namespace: "ns", Name: "viewer"}}}

	ginkgo.It("keeps the request's access token limit on a context of its own", func() {
		request := WithAccessToken(context.NewContext(gocontext.Background()).WithUser(owner), token)
		carried := WithRequestIdentity(context.NewContext(gocontext.Background()), request)

		Expect(carried.User().ID).To(Equal(owner.ID))
		roles, limited := TokenLimit(carried, owner.ID.String())
		Expect(limited).To(BeTrue())
		Expect(roles).To(Equal(token.Roles))
		Expect(AccessTokenCacheKey(carried)).To(Equal(AccessTokenCacheKey(request)))
	})

	ginkgo.It("keeps a subject other than the user", func() {
		request := context.NewContext(gocontext.Background()).WithUser(owner).WithSubject("federated:sub")
		carried := WithRequestIdentity(context.NewContext(gocontext.Background()), request)
		Expect(carried.Subject()).To(Equal("federated:sub"))
		Expect(MadeWithAccessToken(carried)).To(BeFalse())
	})
})
