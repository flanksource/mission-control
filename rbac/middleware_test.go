package rbac

import (
	"encoding/json"
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
		object       string
		action       string
		expectedCode int
		expectedBody string
	}{
		{"GET", "/db/identities", "", ObjectDatabase, "any", http.StatusUnauthorized, errNoUserID.Error()},
		{path: "/db/identities", method: http.MethodGet, user: "admin", expectedCode: http.StatusOK, expectedBody: successBody, object: ObjectDatabase, action: "any"},
		{path: "/db/checks", method: http.MethodGet, user: "viewer", expectedCode: http.StatusOK, expectedBody: successBody, object: ObjectDatabase, action: "any"},
		{path: "/db/canaries", method: http.MethodGet, user: "viewer", expectedCode: http.StatusForbidden, expectedBody: errAccessDenied, object: ObjectDatabase, action: "any"},
		{path: "/db/canaries", method: http.MethodGet, user: "responder", expectedCode: http.StatusForbidden, expectedBody: errAccessDenied, object: ObjectDatabase, action: "any"},
		{path: "/db/canaries?id=eq.5", method: http.MethodGet, user: "editor", expectedCode: http.StatusOK, expectedBody: successBody, object: ObjectDatabase, action: "any"},
		{path: "/db/comments", method: http.MethodPost, user: "viewer", expectedCode: http.StatusForbidden, expectedBody: errAccessDenied, object: ObjectDatabase, action: "any"},
		{path: "/db/comments", method: http.MethodPost, user: "responder", expectedCode: http.StatusOK, expectedBody: successBody, object: ObjectDatabase, action: "any"},
		{path: "/db/incidents", method: http.MethodPatch, user: "responder", expectedCode: http.StatusOK, expectedBody: successBody, object: ObjectDatabase, action: "any"},
		{path: "/db/incidents", method: http.MethodPost, user: "responder", expectedCode: http.StatusForbidden, expectedBody: errAccessDenied, object: ObjectDatabase, action: "any"},
		{path: "/db/incidents", method: http.MethodPost, user: "commander", expectedCode: http.StatusOK, expectedBody: successBody, object: ObjectDatabase, action: "any"},
		{path: "/auth/invite_user", method: http.MethodPost, user: "commander", expectedCode: http.StatusForbidden, expectedBody: errAccessDenied, object: ObjectAuth, action: ActionWrite},
		{path: "/auth/invite_user", method: http.MethodPost, user: "admin", expectedCode: http.StatusOK, expectedBody: successBody, object: ObjectAuth, action: ActionWrite},
		{path: "/bad/config", method: http.MethodPost, user: "admin", expectedCode: http.StatusOK, expectedBody: successBody, object: "", action: "random"},
		{path: "/bad/config", method: http.MethodPost, user: "editor", expectedCode: http.StatusForbidden, expectedBody: errMisconfiguredRBAC, object: "", action: "any"},
		{path: "/bad/config", method: http.MethodPost, user: "editor", expectedCode: http.StatusForbidden, expectedBody: errMisconfiguredRBAC, object: "any", action: ""},
		{path: "/bad/config", method: http.MethodPost, user: "editor", expectedCode: http.StatusForbidden, expectedBody: errAccessDenied, object: "unknown", action: "unknown"},
		{path: "/no/user", method: http.MethodPost, user: "", expectedCode: http.StatusUnauthorized, expectedBody: errNoUserID.Error(), object: ObjectDatabase, action: "any"},
	}

	for _, tc := range tests {

		It(fmt.Sprintf("%s %s %s", tc.method, tc.path, tc.user), func() {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set(api.UserIDHeaderKey, tc.user)
			rec := httptest.NewRecorder()

			// Call endpoint
			req = req.WithContext(DefaultContext)
			_ = Authorization(tc.object, tc.action)(handler)(e.NewContext(req, rec))
			Expect(rec.Code).To(Equal(tc.expectedCode))

			if rec.Body.String() != tc.expectedBody {
				var httpError map[string]string
				err := json.Unmarshal(rec.Body.Bytes(), &httpError)
				Expect(err).To(BeNil())
				Expect(tc.expectedBody).To(Equal(httpError["error"]))
			}
		})
	}
})
