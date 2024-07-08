package rbac

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/incident-commander/api"
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

})
var _ = AfterSuite(setup.AfterSuiteFn)

var _ = Describe("Authorization", func() {

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

	e := echo.New()
	successBody := "OK"
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, successBody)
	}

	tests := []struct {
		method       string
		path         string
		user         string
		expectedCode int
		expectedBody string
		object       string
		action       string
	}{
		{"GET", "/db/identities", "", http.StatusUnauthorized, errNoUserID.Error(), ObjectDatabase, "any"},
		{"GET", "/db/checks", "viewer", http.StatusOK, successBody, ObjectDatabase, "any"},
		{"GET", "/db/canaries", "viewer", http.StatusForbidden, errAccessDenied.Error(), ObjectDatabase, "any"},
		{"GET", "/db/canaries", "responder", http.StatusForbidden, errAccessDenied.Error(), ObjectDatabase, "any"},
		{"GET", "/db/canaries?id=eq.5", "editor", http.StatusOK, successBody, ObjectDatabase, "any"},
		{"POST", "/db/comments", "viewer", http.StatusForbidden, errAccessDenied.Error(), ObjectDatabase, "any"},
		{"POST", "/db/comments", "responder", http.StatusOK, successBody, ObjectDatabase, "any"},
		{"POST", "/db/incidents", "responder", http.StatusOK, successBody, ObjectDatabase, "any"},
		{"POST", "/db/incidents", "responder", http.StatusForbidden, errAccessDenied.Error(), ObjectDatabase, "any"},
		{"POST", "/db/incidents", "commander", http.StatusOK, successBody, ObjectDatabase, "any"},
		{"POST", "/auth/invite_user", "commander", http.StatusForbidden, errAccessDenied.Error(), ObjectAuth, ActionWrite},
		{"POST", "/auth/invite_user", "admin", http.StatusOK, successBody, ObjectAuth, ActionWrite},
		{"POST", "/bad/config", "admin", http.StatusOK, successBody, "", "random"},
		{"POST", "/bad/config", "editor", http.StatusForbidden, errMisconfiguredRBAC.Error(), "", "any"},
		{"POST", "/bad/config", "editor", http.StatusForbidden, errMisconfiguredRBAC.Error(), "any", ""},
		{"POST", "/bad/config", "editor", http.StatusForbidden, errAccessDenied.Error(), "unknown", "unknown"},
		{"POST", "/no/user", "", http.StatusUnauthorized, errNoUserID.Error(), ObjectDatabase, "any"},
	}

	forbidden := []struct {
		method string
		path   string
		user   string
	}{

		{"GET", "/catalog/changes", "any"},
	}

	fmt.Printf("%s", forbidden)
	for _, tc := range tests {

		It(fmt.Sprintf("%s %s %s", tc.method, tc.path, tc.user), func() {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set(api.UserIDHeaderKey, tc.user)
			rec := httptest.NewRecorder()

			// Call endpoint
			req = req.WithContext(DefaultContext)
			_ = Authorization(tc.object, tc.action)(handler)(e.NewContext(req, rec))
			Expect(rec.Code).To(Equal(tc.expectedCode))
			Expect(tc.expectedBody).To(Equal(rec.Body.String()))
		})
	}
})
