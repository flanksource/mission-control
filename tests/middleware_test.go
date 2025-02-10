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
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
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

var e *echo.Echo
var successBody = "OK"
var server *httptest.Server

var _ = BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn(setup.WithoutDummyData)
	vars.AuthMode = ""
	e = echoSrv.New(DefaultContext)
	e.Use(auth.MockAuthMiddleware)

	Expect(DefaultContext.DB().Exec("TRUNCATE casbin_rule").Error).To(BeNil())
	if err := dutyRBAC.Init(DefaultContext, "admin"); err != nil {
		Fail(fmt.Sprintf("error instantiating rbac: %v", err))
	}
	usersAndRoles := map[string]string{
		"admin":     policy.RoleAdmin,
		"editor":    policy.RoleEditor,
		"commander": policy.RoleCommander,
		"responder": policy.RoleResponder,
		"viewer":    policy.RoleViewer,
	}

	for user, role := range usersAndRoles {
		DefaultContext.DB().Save(&models.Person{
			Name:  user,
			Email: user + "@test.com",
		})
		if err := dutyRBAC.AddRoleForUser(user, role); err != nil {
			Fail(fmt.Sprintf("error adding roles for users: %v", err))
		}
	}

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

	var never = []string{
		"/db/courier_messages",
		"/db/access_token",
		"/db/identity_credentials",
		"/db/identity_recovery_addresses",
		"/db/identity_recovery_tokens",
		"/db/identity_verifiable_addresses",
		"/db/identity_verification_tokens",
	}

	for _, t := range never {
		tests = append(tests, TC{
			method:       "POST|GET|PUT|DELETE",
			user:         "admin",
			path:         t,
			expectedCode: http.StatusForbidden,
		})
	}

	var privileged = []string{
		// "/auth/invite_user",
		"/db/connections",
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
	}

	for _, t := range privileged {
		tests = append(tests, TC{
			method:       "POST|GET|PUT|DELETE",
			user:         "viewer",
			path:         t,
			expectedCode: http.StatusForbidden,
		})
	}

	var unprivileged = []string{
		"GET /db/identities",
		"GET /db/identities",
		"GET /db/playbooks",
		"GET /db/people",
		"GET /db/agents",
		"GET /db/playbook_names",

		"GET /db/config_statuses",
		"GET /db/rpc/related_configs_recursive",
		"GET /db/config_analysis_items",
		"GET /db/people_roles",
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
		"GET /metrics",
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

	It("Should cover all db objects", func() {
		info := &db.Info{}
		if err := info.Get(DefaultContext.DB()); err != nil {
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(len(info.Functions)).To(BeNumerically(">", 0))

		for _, table := range append(info.Views, info.Tables...) {
			Expect(dutyRBAC.GetObjectByTable(table)).NotTo(BeEmpty(), table)
		}
		for _, function := range info.Functions {
			Expect(dutyRBAC.GetObjectByTable("rpc/"+function)).NotTo(BeEmpty(), function)
		}
	})
})
