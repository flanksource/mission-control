package rbac

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	"github.com/labstack/echo/v4"
)

var postgresServer *embeddedPG.EmbeddedPostgres

const pgUrl = "postgres://postgres:postgres@localhost:9876/test?sslmode=disable"

func TestAuthorization(t *testing.T) {

	postgresServer = embeddedPG.NewDatabase(embeddedPG.DefaultConfig().
		Database("test").
		Port(9876).
		Logger(io.Discard))
	if err := postgresServer.Start(); err != nil {
		t.Fatalf("error starting postgres server: %v", err)
	}
	logger.Infof("Started postgres on port 9876")

	defer func() {
		logger.Infof("Stopping postgres on port 9876")
		if err := postgresServer.Stop(); err != nil {
			t.Fatalf("error stopping postgres server: %v", err)
		}
	}()

	db.Init(pgUrl)
	Init()
	e := echo.New()
	successBody := "OK"
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, successBody)
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
			t.Fatalf("error adding roles for users: %v", err)
		}
	}

	tests := []struct {
		path         string
		method       string
		user         string
		object       string
		action       string
		expectedCode int
		expectedBody string
	}{
		{path: "/db/identities", method: http.MethodGet, user: "", expectedCode: http.StatusUnauthorized, expectedBody: errNoUserID, object: ObjectDatabase, action: "any"},
		{path: "/db/identities", method: http.MethodGet, user: "admin", expectedCode: http.StatusOK, expectedBody: successBody, object: ObjectDatabase, action: "any"},
		{path: "/db/checks", method: http.MethodGet, user: "viewer", expectedCode: http.StatusOK, expectedBody: successBody, object: ObjectDatabase, action: "any"},
		{path: "/db/canaries", method: http.MethodGet, user: "viewer", expectedCode: http.StatusUnauthorized, expectedBody: errAccessDenied, object: ObjectDatabase, action: "any"},
		{path: "/db/canaries", method: http.MethodGet, user: "responder", expectedCode: http.StatusUnauthorized, expectedBody: errAccessDenied, object: ObjectDatabase, action: "any"},
		{path: "/db/canaries", method: http.MethodGet, user: "editor", expectedCode: http.StatusOK, expectedBody: successBody, object: ObjectDatabase, action: "any"},
		{path: "/db/comments", method: http.MethodPost, user: "viewer", expectedCode: http.StatusUnauthorized, expectedBody: errAccessDenied, object: ObjectDatabase, action: "any"},
		{path: "/db/comments", method: http.MethodPost, user: "responder", expectedCode: http.StatusOK, expectedBody: successBody, object: ObjectDatabase, action: "any"},
		{path: "/db/incidents", method: http.MethodPatch, user: "responder", expectedCode: http.StatusOK, expectedBody: successBody, object: ObjectDatabase, action: "any"},
		{path: "/db/incidents", method: http.MethodPost, user: "responder", expectedCode: http.StatusUnauthorized, expectedBody: errAccessDenied, object: ObjectDatabase, action: "any"},
		{path: "/db/incidents", method: http.MethodPost, user: "commander", expectedCode: http.StatusOK, expectedBody: successBody, object: ObjectDatabase, action: "any"},
		{path: "/auth/invite_user", method: http.MethodPost, user: "commander", expectedCode: http.StatusUnauthorized, expectedBody: errAccessDenied, object: ObjectAuth, action: ActionWrite},
		{path: "/auth/invite_user", method: http.MethodPost, user: "admin", expectedCode: http.StatusOK, expectedBody: successBody, object: ObjectAuth, action: ActionWrite},
		{path: "/bad/config", method: http.MethodPost, user: "admin", expectedCode: http.StatusOK, expectedBody: successBody, object: "", action: "random"},
		{path: "/bad/config", method: http.MethodPost, user: "editor", expectedCode: http.StatusUnauthorized, expectedBody: errMisconfiguredRBAC, object: "", action: "any"},
		{path: "/bad/config", method: http.MethodPost, user: "editor", expectedCode: http.StatusUnauthorized, expectedBody: errMisconfiguredRBAC, object: "any", action: ""},
		{path: "/bad/config", method: http.MethodPost, user: "editor", expectedCode: http.StatusUnauthorized, expectedBody: errAccessDenied, object: "unknown", action: "unknown"},
		{path: "/no/user", method: http.MethodPost, user: "", expectedCode: http.StatusUnauthorized, expectedBody: errNoUserID, object: ObjectDatabase, action: "any"},
	}

	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set(auth.UserIDHeaderKey, tc.user)
		rec := httptest.NewRecorder()

		// Call endpoint
		Authorization(tc.object, tc.action)(handler)(e.NewContext(req, rec))

		if rec.Code != tc.expectedCode {
			t.Fatalf("expected: %v, got: %v. For test case: %+v", tc.expectedCode, rec.Code, tc)
		}

		if tc.expectedBody != rec.Body.String() {
			t.Fatalf("expected: %v, got: %v", tc.expectedBody, rec.Body.String())
		}
	}
}
