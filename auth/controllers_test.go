package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"

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

	"github.com/flanksource/incident-commander/rbac/adapter"
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
		if err := rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter); err != nil {
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
				[]string{policy.ObjectCatalog, policy.ActionRead, "allow", "true", "na"},
				[]string{policy.ObjectPlaybooks, policy.ActionRead, "allow", "true", "na"},
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

		It("should create token successfully with all user permissions", func() {
			ctx := DefaultContext.WithUser(testUser)
			reqData := CreateTokenRequest{
				Name:  "test-token",
				Scope: []policy.Permission{},
			}
			_ = postCreateToken(ctx, e, reqData, http.StatusOK)

			token := findTokenByName(ctx, reqData.Name)
			Expect(token.ID.String()).ToNot(Equal(uuid.Nil.String()))

			Expect(canEnforce(token.PersonID.String(), policy.ObjectCatalog, policy.ActionRead)).To(BeTrue())
			Expect(canEnforce(token.PersonID.String(), policy.ObjectPlaybooks, policy.ActionRead)).To(BeTrue())

			Expect(canEnforce(token.PersonID.String(), policy.ObjectTopology, policy.ActionRead)).To(BeFalse())
			Expect(canEnforce(token.PersonID.String(), policy.ObjectCatalog, policy.ActionCRUD)).To(BeFalse())
			Expect(canEnforce(token.PersonID.String(), policy.ObjectCanary, policy.ActionAll)).To(BeFalse())

		})

		It("should create token with denied permissions filtered out", func() {
			reqData := CreateTokenRequest{
				Name: "limited-token",
				Scope: []policy.Permission{
					{
						Subject: testUser.ID.String(),
						Object:  policy.ObjectCatalog,
						Action:  policy.ActionRead,
					},
				},
			}

			ctx := DefaultContext.WithUser(testUser)
			_ = postCreateToken(ctx, e, reqData, http.StatusOK)

			// Get the created token person and verify permissions
			token := findTokenByName(ctx, reqData.Name)
			Expect(token.ID.String()).ToNot(Equal(uuid.Nil.String()))

			Expect(canEnforce(token.PersonID.String(), policy.ObjectCatalog, policy.ActionRead)).To(BeTrue())

			Expect(canEnforce(token.PersonID.String(), policy.ObjectPlaybooks, policy.ActionRead)).To(BeFalse())
			Expect(canEnforce(token.PersonID.String(), policy.ObjectCatalog, policy.ActionCRUD)).To(BeFalse())
			Expect(canEnforce(token.PersonID.String(), policy.ObjectCanary, policy.ActionAll)).To(BeFalse())
		})
	})

	Context("with invalid request", func() {
		It("should return error when user is nil", func() {
			reqData := CreateTokenRequest{
				Name: "test-token",
			}
			_ = postCreateToken(DefaultContext, e, reqData, http.StatusInternalServerError)
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
				Name: "no-perm-token",
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
