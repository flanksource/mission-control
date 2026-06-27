package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/golang-jwt/jwt/v4"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/patrickmn/go-cache"
)

var _ = ginkgo.Describe("Clerk session claims", func() {
	ginkgo.It("does not coerce missing claims into cache identities", func() {
		claims := jwt.MapClaims{}

		Expect(clerkClaimString(claims, "sid")).To(BeEmpty())
		Expect(clerkUserID(claims)).To(BeEmpty())
		Expect(clerkUserCacheKey("org_1", "user_1")).ToNot(Equal(clerkUserCacheKey("org_1", "user_2")))
	})

	ginkgo.It("supports Clerk session token claim shapes", func() {
		claims := jwt.MapClaims{
			"sub": "user_123",
			"o": map[string]any{
				"id":  "org_123",
				"rol": "admin",
			},
		}

		Expect(clerkUserID(claims)).To(Equal("user_123"))
		Expect(clerkOrgID(claims)).To(Equal("org_123"))
		Expect(clerkRole(claims)).To(Equal("admin"))
	})

	ginkgo.It("prefers the relayed callback token over ambient session credentials", func() {
		req := httptest.NewRequest(http.MethodPost, "/oidc/clerk/callback", strings.NewReader("clerk_session_token=relayed-token"))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		req.Header.Set(echo.HeaderAuthorization, "Bearer ambient-token")
		ec := echo.New().NewContext(req, httptest.NewRecorder())

		checker := NewClerkCredentialChecker(&ClerkHandler{})
		Expect(checker.callbackSessionToken(ec)).To(Equal("relayed-token"))
	})

	ginkgo.It("ignores Clerk callback tokens from query parameters", func() {
		req := httptest.NewRequest(http.MethodPost, "/oidc/clerk/callback?clerk_session_token=query-token", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer ambient-token")
		ec := echo.New().NewContext(req, httptest.NewRecorder())

		checker := NewClerkCredentialChecker(&ClerkHandler{})
		Expect(checker.callbackSessionToken(ec)).To(Equal("ambient-token"))
	})

	ginkgo.It("does not reuse a Clerk user cache entry for another user", func() {
		if dutyRBAC.Enforcer() == nil {
			Expect(dutyRBAC.Init(DefaultContext, []string{}, adapter.NewPermissionAdapter)).To(Succeed())
		}

		var first, second AuthResult
		defer func() {
			if first.User != nil {
				_, _ = dutyRBAC.Enforcer().DeleteRolesForUser(first.User.ID.String())
			}
			if second.User != nil {
				_, _ = dutyRBAC.Enforcer().DeleteRolesForUser(second.User.ID.String())
			}
		}()

		orgID := "org_test_" + time.Now().Format("20060102150405")
		userA := "user_a_" + time.Now().Format("150405.000000000")
		userB := "user_b_" + time.Now().Format("150405.000000000")
		defer DefaultContext.DB().Where("external_id IN ?", []string{userA, userB}).Delete(&models.Person{})

		handler := &ClerkHandler{
			orgID:     orgID,
			userCache: cache.New(3*24*time.Hour, 12*time.Hour),
		}

		var err error
		first, err = handler.getUserFromSessionClaims(DefaultContext, jwt.MapClaims{
			"org_id":    orgID,
			"user_id":   userA,
			"email":     fmt.Sprintf("%s@example.com", userA),
			"name":      "User A",
			"image_url": "https://example.com/a.png",
			"role":      "viewer",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(first.SessionID).To(BeEmpty())
		Expect(first.User.ExternalID).To(Equal(userA))

		second, err = handler.getUserFromSessionClaims(DefaultContext, jwt.MapClaims{
			"org_id":    orgID,
			"user_id":   userB,
			"email":     fmt.Sprintf("%s@example.com", userB),
			"name":      "User B",
			"image_url": "https://example.com/b.png",
			"role":      "viewer",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(second.SessionID).To(BeEmpty())
		Expect(second.User.ExternalID).To(Equal(userB))
		Expect(second.User.ID).ToNot(Equal(first.User.ID))
	})
})
