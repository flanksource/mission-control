package rbac

import (
	"fmt"

	"net/http"
	"net/http/httptest"
	"testing"

	httpClient "github.com/flanksource/commons/http"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/incident-commander/api"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/vars"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	DefaultContext context.Context
)

func TestAuthorization(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Authorization")
}

var _ = BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn()
	vars.AuthMode = ""

})
var _ = AfterSuite(setup.AfterSuiteFn)

var _ = Describe("Authorization", func() {
	var e *echo.Echo
	BeforeAll(func() {
		e = echoSrv.New(DefaultContext)
		e.Use(echoSrv.MockAuthMiddleware)
	})

	It("should setup", func() {
		if err := Init(DefaultContext.DB(), "admin"); err != nil {
			Fail(fmt.Sprintf("error instantiating rbac: %v", err))
		}
		usersAndRoles := map[string]string{
			"admin":     RoleAdmin,
			"editor":    RoleEditor,
			"commander": RoleCommander,
			"responder": RoleResponder,
		}

		for user, role := range usersAndRoles {
			_, err := Enforcer.AddRoleForUser(user, role)
			if err != nil {
				Fail(fmt.Sprintf("error adding roles for users: %v", err))
			}
		}
	})

	successBody := "OK"

	tests := []struct {
		method       string
		path         string
		user         string
		expectedCode int
		expectedBody string
	}{
		{"GET", "/db/identities", "", http.StatusUnauthorized, errNoUserID.Error()},
		{"GET", "/db/access_token", "admin", http.StatusUnauthorized, errNoUserID.Error()},
		{"GET", "/db/checks", "viewer", http.StatusOK, successBody},
		{"GET", "/db/canaries", "viewer", http.StatusForbidden, errAccessDenied.Error()},
		{"GET", "/db/canaries", "responder", http.StatusForbidden, errAccessDenied.Error()},
		{"GET", "/db/canaries?id=eq.5", "editor", http.StatusOK, successBody},
		{"POST", "/db/comments", "viewer", http.StatusForbidden, errAccessDenied.Error()},
		{"POST", "/db/comments", "responder", http.StatusOK, successBody},
		{"POST", "/db/incidents", "responder", http.StatusOK, successBody},
		{"POST", "/db/incidents", "responder", http.StatusForbidden, errAccessDenied.Error()},
		{"POST", "/db/incidents", "commander", http.StatusOK, successBody},
		{"POST", "/auth/invite_user", "commander", http.StatusForbidden, errAccessDenied.Error()},
		{"POST", "/auth/invite_user", "admin", http.StatusOK, successBody},
		{"POST", "/bad/config", "admin", http.StatusOK, successBody},
		{"POST", "/bad/config", "editor", http.StatusForbidden, errMisconfiguredRBAC.Error()},
		{"POST", "/bad/config", "editor", http.StatusForbidden, errMisconfiguredRBAC.Error()},
		{"POST", "/bad/config", "editor", http.StatusForbidden, errAccessDenied.Error()},
		{"POST", "/no/user", "", http.StatusUnauthorized, errNoUserID.Error()},
	}

	for _, tc := range tests {

		It(fmt.Sprintf("%s %s %s", tc.method, tc.path, tc.user), func() {
			// Mock all responses with OK
			e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c echo.Context) error {
					return c.String(http.StatusOK, successBody)
				}
			})

			server := httptest.NewServer(e)
			defer server.Close()

			client := httpClient.NewClient().BaseURL(server.URL)

			resp, err := client.
				R(context.Context{}).
				Header(api.UserIDHeaderKey, tc.user).
				Do(tc.method, tc.path)

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(tc.expectedCode))
			Expect(resp.AsString()).To(Equal(tc.expectedBody))
		})
	}
})
