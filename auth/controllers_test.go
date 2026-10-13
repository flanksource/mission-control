package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/incident-commander/db"
	icrbac "github.com/flanksource/incident-commander/rbac"
	"github.com/samber/lo"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func postCreateToken(ctx context.Context, e *echo.Echo, reqData CreateTokenRequest, expectedStatus int) dutyAPI.HTTPSuccess {
	reqBody, _ := json.Marshal(reqData)

	req := httptest.NewRequest(http.MethodPost, "/auth/create_token", bytes.NewBuffer(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	c.SetRequest(req.WithContext(ctx))

	err := CreateToken(c)
	Expect(err).To(BeNil())
	Expect(rec.Code).To(Equal(expectedStatus))

	var response dutyAPI.HTTPSuccess
	if rec.Code != http.StatusOK {
		return response
	}

	err = json.Unmarshal(rec.Body.Bytes(), &response)
	Expect(err).To(BeNil())

	Expect(response.Message).To(Equal("success"))
	Expect(response.Payload).To(HaveKey("token"))
	return response
}

func findTokenByName(ctx context.Context, tokenName string) models.AccessToken {
	token, err := gorm.G[models.AccessToken](ctx.DB()).Where("name = ?", tokenName).First(ctx)
	Expect(err).To(BeNil())
	return token
}

var testRoles = models.AccessTokenRoles{{Namespace: "mission-control-builtin", Name: "viewer"}}

func canEnforce(sub, obj, act string) bool {
	allowed, err := rbac.Enforcer().Enforce(sub, obj, act)
	Expect(err).To(BeNil())
	return allowed
}

var _ = Describe("CreateToken", Ordered, func() {
	var (
		testUser *models.Person
		e        *echo.Echo
		err      error
	)

	BeforeAll(func() {
		// Initialize RBAC with admin user
		if err := icrbac.Init(DefaultContext); err != nil {
			Fail("Failed to initialize RBAC: " + err.Error())
		}

		// Create test users
		testUser = &dummy.JohnDoe
		Expect(err).To(BeNil())

		e = echo.New()
		RegisterRoutes(e)
	})

	Context("with valid user and permissions", func() {
		BeforeEach(func() {
			// Add specific permissions for test user
			_, err = rbac.Enforcer().AddPermissionsForUser(testUser.ID.String(),
				[]string{policy.ObjectCatalog, policy.ActionRead, "allow", "", "na"},
				[]string{policy.ObjectPlaybooks, policy.ActionRead, "allow", "", "na"},
			)
			Expect(err).To(BeNil())
		})

		AfterEach(func() {
			// Clean up permissions
			_, err = rbac.Enforcer().DeleteRolesForUser(testUser.ID.String())
			Expect(err).To(BeNil())
			_, err = rbac.Enforcer().DeletePermissionsForUser(testUser.ID.String())
			Expect(err).To(BeNil())
		})

		It("refuses a token without roles", func() {
			ctx := DefaultContext.WithUser(testUser)
			_ = postCreateToken(ctx, e, CreateTokenRequest{Name: "no-roles-token"}, http.StatusBadRequest)
			_ = postCreateToken(ctx, e, CreateTokenRequest{Name: "no-roles-token", Roles: models.AccessTokenRoles{}}, http.StatusBadRequest)

			_, err := gorm.G[models.AccessToken](DefaultContext.DB()).Where("name = ?", "no-roles-token").First(DefaultContext)
			Expect(err).To(MatchError(gorm.ErrRecordNotFound))
		})

		It("stores the token's roles without writing deny rows", func() {
			reqData := CreateTokenRequest{
				Name:  "limited-token",
				Roles: models.AccessTokenRoles{{Namespace: "anywhere", Name: "readonly"}},
			}

			ctx := DefaultContext.WithUser(testUser)
			_ = postCreateToken(ctx, e, reqData, http.StatusOK)

			token := findTokenByName(ctx, reqData.Name)
			Expect(token.Roles).To(Equal(reqData.Roles))

			own, err := rbac.Enforcer().GetPermissionsForUser(token.PersonID.String())
			Expect(err).To(BeNil())
			Expect(own).To(BeEmpty(), "a token's limit is its roles, not deny rows")
			Expect(canEnforce(token.PersonID.String(), policy.ObjectCatalog, policy.ActionRead)).To(BeTrue())
		})

		It("refuses to create or delete a token with a token", func() {
			ctx := DefaultContext.WithUser(testUser)
			_ = postCreateToken(ctx, e, CreateTokenRequest{Name: "first-token", Roles: testRoles}, http.StatusOK)
			first := findTokenByName(ctx, "first-token")

			var tokenPerson models.Person
			Expect(DefaultContext.DB().Where("id = ?", first.PersonID).First(&tokenPerson).Error).To(Succeed())
			tokenCtx := withAccessToken(DefaultContext.WithUser(&tokenPerson))

			_ = postCreateToken(tokenCtx, e, CreateTokenRequest{Name: "second-token", Roles: testRoles}, http.StatusForbidden)
			_, err := gorm.G[models.AccessToken](DefaultContext.DB()).Where("name = ?", "second-token").First(DefaultContext)
			Expect(err).To(MatchError(gorm.ErrRecordNotFound))

			req := httptest.NewRequest(http.MethodDelete, "/auth/token/"+first.ID.String(), nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetRequest(req.WithContext(tokenCtx))
			c.SetParamNames("id")
			c.SetParamValues(first.ID.String())
			Expect(DeleteToken(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusForbidden))
			Expect(findTokenByName(ctx, "first-token").ID).To(Equal(first.ID))
		})
	})

	Context("with invalid request", func() {
		It("should return error when user is nil", func() {
			reqData := CreateTokenRequest{
				Name: "test-token",
			}
			_ = postCreateToken(DefaultContext, e, reqData, http.StatusUnauthorized)
		})
	})

	Context("RBAC enforcer permission validation", func() {
		It("should handle user with no permissions", func() {
			// Create a user with no permissions
			noPermUser := &models.Person{
				ID:    uuid.New(),
				Name:  "No Permissions User",
				Email: "noperm@test",
			}
			err := DefaultContext.DB().Create(noPermUser).Error
			Expect(err).To(BeNil())

			reqData := CreateTokenRequest{
				Name:  "no-perm-token",
				Roles: testRoles,
			}

			ctx := DefaultContext.WithUser(noPermUser)
			_ = postCreateToken(ctx, e, reqData, http.StatusOK)
			token := findTokenByName(ctx, reqData.Name)
			Expect(token.ID.String()).ToNot(Equal(uuid.Nil.String()))

			perms, err := rbac.PermsForUser(token.PersonID.String())
			Expect(err).To(BeNil())
			Expect(perms).To(HaveLen(0), "Token should have no permissions")
		})
	})
})

var _ = Describe("Managing access tokens", Ordered, func() {
	var (
		e                        *echo.Echo
		owner, other, agentAdmin *models.Person
		ownToken, agentToken     *models.AccessToken
	)

	as := func(person *models.Person, method, path string) *httptest.ResponseRecorder {
		GinkgoHelper()
		req := httptest.NewRequest(method, path, nil)
		req = req.WithContext(DefaultContext.WithUser(person))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	listed := func(person *models.Person) []uuid.UUID {
		GinkgoHelper()
		rec := as(person, http.MethodGet, "/auth/tokens")
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		var body struct {
			Payload []models.AccessToken `json:"payload"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		return lo.Map(body.Payload, func(t models.AccessToken, _ int) uuid.UUID { return t.ID })
	}

	BeforeAll(func() {
		Expect(icrbac.Init(DefaultContext)).To(Succeed())
		e = echo.New()
		RegisterRoutes(e)

		var err error
		for _, p := range []**models.Person{&owner, &other, &agentAdmin} {
			*p, err = db.CreatePerson(DefaultContext, "Token Manager "+uuid.NewString()[:8], uuid.NewString()[:8]+"@tokens.test", "")
			Expect(err).ToNot(HaveOccurred())
		}
		_, err = rbac.Enforcer().AddPolicy(agentAdmin.ID.String(), policy.ObjectAgent, policy.ActionUpdate, "allow", "", "test")
		Expect(err).ToNot(HaveOccurred())

		_, ownToken, err = db.CreateAccessToken(DefaultContext, owner.ID, "owned", nil, &owner.ID, false)
		Expect(err).ToNot(HaveOccurred())

		agent, err := db.CreatePerson(DefaultContext, "agent-"+uuid.NewString()[:8], uuid.NewString()[:8]+"@local", db.PersonTypeAgent)
		Expect(err).ToNot(HaveOccurred())
		_, agentToken, err = db.CreateAccessToken(DefaultContext, agent.ID, "default", nil, nil, false)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterAll(func() {
		_, err := rbac.Enforcer().DeletePermissionsForUser(agentAdmin.ID.String())
		Expect(err).ToNot(HaveOccurred())
	})

	It("lists a person's own tokens without a grant, and agents' tokens to those who may update agents", func() {
		Expect(listed(owner)).To(Equal([]uuid.UUID{ownToken.ID}))
		Expect(listed(other)).To(BeEmpty())
		Expect(listed(agentAdmin)).To(ContainElement(agentToken.ID))
		Expect(listed(agentAdmin)).ToNot(ContainElement(ownToken.ID))
	})

	It("lets only the creator delete a person's token", func() {
		Expect(as(other, http.MethodDelete, "/auth/token/"+ownToken.ID.String()).Code).To(Equal(http.StatusForbidden))
		Expect(as(agentAdmin, http.MethodDelete, "/auth/token/"+ownToken.ID.String()).Code).To(Equal(http.StatusForbidden))
		Expect(as(owner, http.MethodDelete, "/auth/token/"+ownToken.ID.String()).Code).To(Equal(http.StatusOK))
	})

	It("revokes an agent's token with update on agent", func() {
		Expect(as(other, http.MethodDelete, "/auth/token/"+agentToken.ID.String()).Code).To(Equal(http.StatusForbidden))
		Expect(as(agentAdmin, http.MethodDelete, "/auth/token/"+agentToken.ID.String()).Code).To(Equal(http.StatusOK))
	})
})
