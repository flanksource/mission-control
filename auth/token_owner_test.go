package auth

import (
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/patrickmn/go-cache"

	"github.com/flanksource/incident-commander/auth/accesstoken"
	"github.com/flanksource/incident-commander/db"
	icrbac "github.com/flanksource/incident-commander/rbac"
)

// signedIn is what a request made with an access token is signed in as.
type signedIn struct {
	code          int
	user          uuid.UUID
	madeWithToken bool
	limit         models.AccessTokenRoles
	limited       bool
	otherLimited  bool
}

var _ = ginkgo.Describe("Access tokens sign in as their owner", ginkgo.Ordered, func() {
	var owner *models.Person

	kratos := &kratosMiddleware{
		tokenCache:         cache.New(time.Hour, time.Hour),
		accessTokenCache:   cache.New(time.Hour, time.Hour),
		authSessionCache:   cache.New(time.Hour, time.Hour),
		basicAuthSeparator: "separator",
	}
	clerk := ClerkHandler{
		tokenCache:       cache.New(time.Hour, time.Hour),
		accessTokenCache: cache.New(time.Hour, time.Hour),
		userCache:        cache.New(time.Hour, time.Hour),
	}

	modes := []struct {
		name       string
		middleware echo.MiddlewareFunc
		auth       func(token string) func(*http.Request)
	}{
		{"kratos basic auth", kratos.Session, func(t string) func(*http.Request) { return func(r *http.Request) { r.SetBasicAuth("token", t) } }},
		{"kratos bearer", kratos.Session, func(t string) func(*http.Request) {
			return func(r *http.Request) { r.Header.Set(echo.HeaderAuthorization, "Bearer "+t) }
		}},
		{"clerk basic auth", clerk.Session, func(t string) func(*http.Request) { return func(r *http.Request) { r.SetBasicAuth("token", t) } }},
		{"clerk bearer", clerk.Session, func(t string) func(*http.Request) {
			return func(r *http.Request) { r.Header.Set(echo.HeaderAuthorization, "Bearer "+t) }
		}},
		{"basic bearer", basicAuthMiddleware, func(t string) func(*http.Request) {
			return func(r *http.Request) { r.Header.Set(echo.HeaderAuthorization, "Bearer "+t) }
		}},
	}

	signIn := func(middleware echo.MiddlewareFunc, setAuth func(*http.Request)) signedIn {
		ginkgo.GinkgoHelper()
		req := httptest.NewRequest(http.MethodGet, "/resources", nil)
		req = req.WithContext(DefaultContext)
		setAuth(req)
		rec := httptest.NewRecorder()

		var got signedIn
		handler := middleware(func(c echo.Context) error {
			ctx := c.Request().Context().(context.Context)
			got.user = ctx.User().ID
			got.madeWithToken = madeWithAccessToken(ctx)
			got.limit, got.limited = icrbac.TokenLimit(ctx, ctx.User().ID.String())
			_, got.otherLimited = icrbac.TokenLimit(ctx, uuid.NewString())
			return c.NoContent(http.StatusOK)
		})
		Expect(handler(echo.New().NewContext(req, rec))).To(Succeed())
		got.code = rec.Code
		return got
	}

	ginkgo.BeforeAll(func() {
		localhostOnly = false
		var err error
		owner, err = db.CreatePerson(DefaultContext, "Token Owner", "token-owner-"+uuid.NewString()[:8]+"@example.com", "")
		Expect(err).ToNot(HaveOccurred())
	})

	ginkgo.It("creates no person for a token", func() {
		var before, after int64
		Expect(DefaultContext.DB().Model(&models.Person{}).Count(&before).Error).To(Succeed())
		_, token, err := db.CreateAccessTokenForPerson(DefaultContext, owner, "no-person", 0, false, viewerRole)
		Expect(err).ToNot(HaveOccurred())
		Expect(DefaultContext.DB().Model(&models.Person{}).Count(&after).Error).To(Succeed())

		Expect(after).To(Equal(before))
		Expect(token.PersonID).To(Equal(owner.ID))
		Expect(token.CreatedBy).To(Equal(&owner.ID))
	})

	for _, mode := range modes {
		ginkgo.It(mode.name+": limits the owner by the token's Roles, and stops at once when the token is deleted", func() {
			token, row, err := db.CreateAccessTokenForPerson(DefaultContext, owner, "sign-in-"+uuid.NewString()[:8], 0, false, viewerRole)
			Expect(err).ToNot(HaveOccurred())

			got := signIn(mode.middleware, mode.auth(token.PlainText()))
			Expect(got.code).To(Equal(http.StatusOK))
			Expect(got.user).To(Equal(owner.ID))
			Expect(got.madeWithToken).To(BeTrue())
			Expect(got.limited).To(BeTrue())
			Expect(got.limit).To(Equal(viewerRole))
			Expect(got.otherLimited).To(BeFalse(), "another subject isn't limited by the token")

			Expect(DeleteAccessToken(DefaultContext, row.ID.String())).To(Succeed())
			Expect(signIn(mode.middleware, mode.auth(token.PlainText())).code).To(Equal(http.StatusUnauthorized))
		})
	}

	ginkgo.It("drops the RLS payloads cached for a deleted token, but not its owner's", func() {
		_, row, err := db.CreateAccessTokenForPerson(DefaultContext, owner, "rls-cache", 0, false, viewerRole)
		Expect(err).ToNot(HaveOccurred())

		ownerCtx := DefaultContext.WithUser(owner)
		_, err = GetRLSPayload(ownerCtx)
		Expect(err).ToNot(HaveOccurred())
		_, err = GetRLSPayload(icrbac.WithAccessToken(ownerCtx, row))
		Expect(err).ToNot(HaveOccurred())

		tokenKey := getRLSCacheKey(owner.ID.String()) + icrbac.AccessTokenIDCacheKey(row.ID)
		_, cached := tokenCache.Get(tokenKey)
		Expect(cached).To(BeTrue())

		Expect(DeleteAccessToken(DefaultContext, row.ID.String())).To(Succeed())
		_, cached = tokenCache.Get(tokenKey)
		Expect(cached).To(BeFalse())
		_, cached = tokenCache.Get(getRLSCacheKey(owner.ID.String()))
		Expect(cached).To(BeTrue())
	})

	ginkgo.It("moves a token of a person of its own to its owner on startup", func() {
		legacyPerson := func() *models.Person {
			ginkgo.GinkgoHelper()
			person, err := db.CreatePerson(DefaultContext, "Token Owner (Token)", "token-owner+token:"+uuid.NewString()[:8]+"@example.com", "access_token")
			Expect(err).ToNot(HaveOccurred())
			return person
		}
		legacyToken := func(person *models.Person, createdBy *uuid.UUID) (string, models.AccessToken) {
			ginkgo.GinkgoHelper()
			token, err := accesstoken.Generate()
			Expect(err).ToNot(HaveOccurred())
			row := models.AccessToken{Name: "legacy-" + uuid.NewString()[:8], Value: token.Hash(), PersonID: person.ID, CreatedBy: createdBy, Roles: viewerRole}
			Expect(DefaultContext.DB().Create(&row).Error).To(Succeed())
			return token.V2().PlainText(), row
		}

		tokenPerson, orphanPerson, clashPerson := legacyPerson(), legacyPerson(), legacyPerson()
		plain, token := legacyToken(tokenPerson, &owner.ID)
		_, orphan := legacyToken(orphanPerson, nil)

		// The owner already has a token of the name another of their tokens had on its own person
		_, clash := legacyToken(clashPerson, &owner.ID)
		_, taken, err := db.CreateAccessTokenForPerson(DefaultContext, owner, clash.Name, 0, false, viewerRole)
		Expect(err).ToNot(HaveOccurred())

		_, err = dutyRBAC.Enforcer().AddGroupingPolicy(tokenPerson.ID.String(), owner.ID.String())
		Expect(err).ToNot(HaveOccurred())
		_, err = dutyRBAC.Enforcer().AddPolicy(tokenPerson.ID.String(), policy.ObjectCatalog, policy.ActionRead, "allow", "", "na")
		Expect(err).ToNot(HaveOccurred())
		_, err = dutyRBAC.Enforcer().AddGroupingPolicy(orphanPerson.ID.String(), owner.ID.String())
		Expect(err).ToNot(HaveOccurred())

		rulesOf := func(person *models.Person) int64 {
			var count int64
			Expect(DefaultContext.DB().Table("casbin_rule").Where("v0 = ?", person.ID.String()).Count(&count).Error).To(Succeed())
			return count
		}
		Expect(rulesOf(tokenPerson)).To(BeNumerically(">", 0))

		// Twice: the second run finds nothing left to move
		for range 2 {
			Expect(MigrateLegacyTokenPeople(DefaultContext)).To(Succeed())

			moved, err := db.GetAccessToken(DefaultContext, token.ID.String())
			Expect(err).ToNot(HaveOccurred())
			Expect(moved.PersonID).To(Equal(owner.ID))

			var count int64
			Expect(DefaultContext.DB().Model(&models.AccessToken{}).Where("id = ?", orphan.ID).Count(&count).Error).To(Succeed())
			Expect(count).To(BeZero(), "a token without an owner is deleted")

			renamed, err := db.GetAccessToken(DefaultContext, clash.ID.String())
			Expect(err).ToNot(HaveOccurred())
			Expect(renamed.PersonID).To(Equal(owner.ID))
			Expect(renamed.Name).To(Equal(clash.Name + " (migrated " + clash.ID.String() + ")"))
			kept, err := db.GetAccessToken(DefaultContext, taken.ID.String())
			Expect(err).ToNot(HaveOccurred())
			Expect(kept.Name).To(Equal(clash.Name))

			for _, person := range []*models.Person{tokenPerson, orphanPerson, clashPerson} {
				var deleted int64
				Expect(DefaultContext.DB().Model(&models.Person{}).Where("id = ? AND deleted_at IS NOT NULL", person.ID).Count(&deleted).Error).To(Succeed())
				Expect(deleted).To(Equal(int64(1)))

				Expect(rulesOf(person)).To(BeZero())
				roles, err := dutyRBAC.Enforcer().GetRolesForUser(person.ID.String())
				Expect(err).ToNot(HaveOccurred())
				Expect(roles).To(BeEmpty())
				permissions, err := dutyRBAC.Enforcer().GetPermissionsForUser(person.ID.String())
				Expect(err).ToNot(HaveOccurred())
				Expect(permissions).To(BeEmpty())
			}
		}

		got := signIn(modes[1].middleware, modes[1].auth(plain))
		Expect(got.code).To(Equal(http.StatusOK))
		Expect(got.user).To(Equal(owner.ID))
		Expect(got.limited).To(BeTrue())
		Expect(got.limit).To(Equal(viewerRole))
	})
})
