package tests

import (
	"fmt"
	"strings"

	"net/http"
	"net/http/httptest"
	"testing"

	httpClient "github.com/flanksource/commons/http"
	"github.com/flanksource/commons/logger"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/tests/setup"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/auth/signing"
	"github.com/flanksource/incident-commander/db"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/rbac/rbactest"
	"github.com/flanksource/incident-commander/rbac/shipped"
	"github.com/flanksource/incident-commander/vars"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"
)

var (
	DefaultContext context.Context
)

func TestAuthorization(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Authorization")
}

var e *echo.Echo
var successBody = "OK"
var server *httptest.Server

var _ = BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn(setup.WithoutDummyData)
	if _, _, err := signing.Initialize("/tmp/dummy"); err != nil {
		Fail(err.Error())
	}

	vars.AuthMode = ""
	e = echoSrv.New(DefaultContext)
	e.Use(auth.MockAuthMiddleware)

	Expect(DefaultContext.DB().Exec("TRUNCATE casbin_rule").Error).To(BeNil())
	if err := rbac.Init(DefaultContext); err != nil {
		Fail(fmt.Sprintf("error instantiating rbac: %v", err))
	}
	people := map[string]*models.Person{}
	for _, user := range []string{"admin", "viewer"} {
		person := models.Person{Name: user, Email: user + "@test.com"}
		Expect(DefaultContext.DB().Save(&person).Error).To(Succeed())
		people[user] = &person
	}

	// admin is in the shipped admins team, and viewer is bound to the shipped viewer Role
	Expect(rbactest.MakeAdmins(DefaultContext, people["admin"].ID)).To(Succeed())
	Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "viewers", Namespace: adapter.ShippedNamespace, UID: k8sTypes.UID(uuid.NewString())},
		Spec: v1.RoleBindingSpec{Role: shipped.RoleViewer, Subjects: v1.RoleBindingSubjects{
			PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{people["viewer"].Email}},
		}},
	})).To(Succeed())
	Expect(dutyRBAC.ReloadPolicy()).To(Succeed())

	// Mock all responses with OK
	e.Any("/db/*", func(c echo.Context) error {
		return c.String(http.StatusOK, successBody)
	}, rbac.DbMiddleware())
	// // Mock all responses with OK
	// e.Any("/*", func(c echo.Context) error {
	// 	return c.String(http.StatusOK, successBody)
	// })

	for _, r := range e.Routes() {
		if r.Method != "echo_route_not_found" {
			logger.Infof("%s %s -> %s", r.Method, r.Path, r.Name)
		}
	}
	server = httptest.NewServer(e)
	logger.Infof("Started test server @ %s", server.URL)
})

var _ = AfterSuite(func() {
	setup.AfterSuiteFn()
	server.Close()
})

type TC struct {
	method       string
	path         string
	user         string
	expectedCode int
	expectedBody string
}

var _ = Describe("Authorization", func() {

	tests := []TC{}

	// Tables that are neither a type, a record of one, a lookup view of one, nor read whole through an action
	// aren't served, even to admin
	var never = []string{
		"/db/courier_messages",
		"/db/access_token",
		"/db/identity_credentials",
		"/db/identity_recovery_addresses",
		"/db/identity_recovery_tokens",
		"/db/identity_verifiable_addresses",
		"/db/identity_verification_tokens",
		"/db/casbin_rule",
		"/db/migration_logs",
		"/db/scope_members",
		"/db/rpc/uuid_to_ulid",
		"/db/no_such_table",
	}

	for _, t := range never {
		tests = append(tests, TC{
			method:       "POST|GET|PATCH|DELETE",
			user:         "admin",
			path:         t,
			expectedCode: http.StatusNotFound,
		})
	}

	// One table of each kind /db serves
	var served = []string{
		"GET /db/config_items",
		"GET /db/config_changes",
		"GET /db/config_types",
		"GET /db/job_history",
		"GET /db/event_queue",
		"GET /db/properties",
		"PATCH /db/properties",
		"GET /db/view_default_pods",
		"GET /db/incidents",
	}

	for _, t := range served {
		tests = append(tests, TC{
			user:         "admin",
			path:         t,
			expectedCode: http.StatusOK,
		})
	}

	var privileged = []string{
		// "/auth/invite_user",
		"/db/config_scrapers",
		"GET /db/job_history_names",
		"POST|PUT|DELETE /db/topology",
		"POST|PUT|DELETE /db/playbooks",
		"POST|PUT|DELETE /db/canaries",
		"POST|PUT|DELETE /db/check_statuses",
		"POST|PUT|DELETE /db/config_analysis",
		"POST|PUT|DELETE /db/config_changes",
		"POST|PUT|DELETE /db/configs",
		"GET /db/job_history",
		"GET /db/job_history_latest_status",
		"GET /db/integrations_with_status",
		"GET /db/integrations_with_status",
		"GET /db/integrations",
		"GET /db/event_queue_summary",
		"POST|PUT|DELETE /db/config_items",

		// The viewer Role reads nothing that configures access or exposes Mission Control's internals
		"GET /db/agents",

		// No Role grants database.public, which lookup tables aren't mapped from to their types yet
		"GET /db/identities",
		"GET /db/people_roles",
		"GET /db/playbook_names",
	}

	for _, t := range privileged {
		tests = append(tests, TC{
			method:       "POST|GET|PUT|DELETE",
			user:         "viewer",
			path:         t,
			expectedCode: http.StatusForbidden,
		})
	}

	// Connections are only read through connection_details and written through /connection
	for _, user := range []string{"admin", "viewer"} {
		tests = append(tests, TC{
			method:       "POST|GET|PATCH|DELETE",
			user:         user,
			path:         "/db/connections",
			expectedCode: http.StatusMethodNotAllowed,
		})
	}

	var unprivileged = []string{
		"GET /db/playbooks",
		"GET /db/people",

		"GET /db/config_statuses",
		"GET /db/rpc/related_configs_recursive",
		"GET /db/config_analysis_items",
		"GET /db/canaries",
		"GET /db/checks",
		"GET /db/config_items",
		"GET /db/configs",
		"GET /db/config_changes",
	}

	for _, t := range unprivileged {
		tests = append(tests, TC{
			method:       "POST|GET|PUT|DELETE",
			user:         "viewer",
			path:         t,
			expectedCode: http.StatusOK,
		})
	}

	var anonymous = []string{
		"GET /health",
		"GET /properties",
	}

	for _, t := range anonymous {
		tests = append(tests, TC{
			method:       "POST|GET|PUT|DELETE",
			path:         t,
			expectedCode: http.StatusOK,
		})
	}

	for _, tc := range tests {
		if strings.Contains(tc.path, " ") {
			tc.method = strings.Split(tc.path, " ")[0]
			tc.path = strings.Split(tc.path, " ")[1]
		}

		for _, method := range strings.Split(tc.method, "|") {
			m := method

			It(fmt.Sprintf("%s %s %s", m, tc.path, tc.user), func() {
				client := httpClient.NewClient().BaseURL(server.URL)
				if tc.user != "" {
					client = client.Auth(tc.user, tc.user)
				}

				resp, err := client.
					R(context.New()).
					Do(m, tc.path)

				Expect(err).ToNot(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(tc.expectedCode))
				if tc.expectedBody != "" {
					Expect(resp.AsString()).To(Equal(tc.expectedBody))
				}
			})
		}
	}
})
