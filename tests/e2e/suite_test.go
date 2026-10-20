package e2e

import (
	gocontext "context"
	"fmt"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	icrbac "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"

	"github.com/flanksource/commons-test/container"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/samber/lo"
	"github.com/samber/oops"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/auth/signing"
	"github.com/flanksource/incident-commander/db"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/metrics"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/flanksource/incident-commander/playbook/sdk"
	"github.com/flanksource/incident-commander/playbook/testdata"
	"github.com/flanksource/incident-commander/vars"

	// register event handlers
	_ "github.com/flanksource/incident-commander/notification"
)

func TestPlaybook(t *testing.T) {
	tempPath = fmt.Sprintf("%s/config-class.txt", t.TempDir())
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Playbook")
}

var (
	// tempPath is used to store the result of the action for this test.
	tempPath       string
	e              *echo.Echo
	server         *httptest.Server
	metricsServer  *httptest.Server
	DefaultContext context.Context
	client         sdk.PlaybookAPI

	lokiEndpoint        string
	openSearchEndpoint  string
	facetContainer      *container.Container
	lokiContainer       *container.Container
	opensearchContainer *container.Container
)

func startContainers() {
	ctx := gocontext.Background()
	var wg sync.WaitGroup
	var errs [3]error

	wg.Add(3)

	go func() {
		defer wg.Done()
		defer ginkgo.GinkgoRecover()
		var err error
		lokiContainer, err = container.New(container.Config{
			Image: "grafana/loki:3.0.0",
			Name:  "e2e-loki",
			Ports: map[string]string{"3100": "3100"},
			Reuse: true,
			HealthCheck: &container.HealthCheck{
				Cmd:         "wget --no-verbose --tries=1 --spider http://localhost:3100/ready || exit 1",
				Interval:    10 * time.Second,
				Timeout:     10 * time.Second,
				Retries:     5,
				StartPeriod: 15 * time.Second,
			},
		})
		if err == nil {
			err = lokiContainer.Start(ctx)
		}
		errs[0] = err
	}()

	go func() {
		defer wg.Done()
		defer ginkgo.GinkgoRecover()
		var err error
		opensearchContainer, err = container.New(container.Config{
			Image: "opensearchproject/opensearch:3",
			Name:  "e2e-opensearch",
			Ports: map[string]string{"9200": "9200"},
			Env: []string{
				"discovery.type=single-node",
				"DISABLE_INSTALL_DEMO_CONFIG=true",
				"DISABLE_SECURITY_PLUGIN=true",
			},
			Reuse: true,
			HealthCheck: &container.HealthCheck{
				Cmd:         "curl -sf http://localhost:9200/_cluster/health || exit 1",
				Interval:    10 * time.Second,
				Timeout:     10 * time.Second,
				Retries:     5,
				StartPeriod: 15 * time.Second,
			},
		})
		if err == nil {
			err = opensearchContainer.Start(ctx)
		}
		errs[1] = err
	}()

	go func() {
		defer wg.Done()
		defer ginkgo.GinkgoRecover()
		var err error
		facetContainer, err = container.New(container.Config{
			Image: "ghcr.io/flanksource/facet:0.1.69",
			Name:  "e2e-facet",
			Ports: map[string]string{"3010": "0"},
			HealthCheck: &container.HealthCheck{
				Cmd:         "curl -sf http://localhost:3010/healthz || exit 1",
				Interval:    10 * time.Second,
				Timeout:     10 * time.Second,
				Retries:     5,
				StartPeriod: 15 * time.Second,
			},
			Reuse: true,
		})
		if err == nil {
			err = facetContainer.Start(ctx)
		}
		errs[2] = err
	}()

	wg.Wait()

	for _, err := range errs {
		Expect(err).ToNot(HaveOccurred())
	}

	lokiEndpoint = "http://localhost:3100"
	openSearchEndpoint = "http://localhost:9200"
}

func stopContainers() {
	ctx := gocontext.Background()
	if os.Getenv("KEEP") != "true" {
		for _, c := range []*container.Container{facetContainer, lokiContainer, opensearchContainer} {
			if c != nil {
				_ = c.Cleanup(ctx)
			}
		}
	}
}

// artifactReader is the ID of the person artifacts are read as in fixtures
var artifactReader string

// accessGrant is one rule of the Role grantAccess binds: an action on the resources a Scope selects.
type accessGrant struct {
	Action   string
	Resource v1.ScopeTarget
}

// grantAccess binds the subjects to a Role made of the grants, each on a Scope of its own, in the default namespace,
// and reloads the policy. It returns a function that removes them.
func grantAccess(name string, subjects v1.RoleBindingSubjects, grants ...accessGrant) func() {
	ginkgo.GinkgoHelper()

	const namespace = "default"
	meta := func(n string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: n, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())}
	}

	var rules []v1.RoleRule
	for i, grant := range grants {
		scope := fmt.Sprintf("%s-%d", name, i)
		Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{
			ObjectMeta: meta(scope),
			Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{grant.Resource}},
		})).To(Succeed())
		rules = append(rules, v1.RoleRule{Name: scope, Action: grant.Action, Resource: &v1.ScopeReference{ScopeRef: scope}})
	}

	Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{ObjectMeta: meta(name), Spec: v1.RoleSpec{Rules: rules}})).To(Succeed())
	Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
		ObjectMeta: meta(name),
		Spec:       v1.RoleBindingSpec{Role: name, Subjects: subjects},
	})).To(Succeed())
	Expect(dutyRBAC.ReloadPolicy()).To(Succeed())

	return func() {
		ginkgo.GinkgoHelper()
		Expect(DefaultContext.DB().Where("namespace = ? AND name = ?", namespace, name).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ? AND name = ?", namespace, name).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ? AND name LIKE ?", namespace, name+"-%").Delete(&models.Scope{}).Error).To(Succeed())
		Expect(dutyRBAC.ReloadPolicy()).To(Succeed())
	}
}

var allConnections = v1.ScopeTarget{Connection: &v1.ScopeConnectionSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}}

var _ = ginkgo.BeforeSuite(func() {
	startContainers()

	if _, _, err := signing.Initialize("/tmp/dummy"); err != nil {
		ginkgo.Fail(err.Error())
	}

	format.RegisterCustomFormatter(func(value interface{}) (string, bool) {
		switch v := value.(type) {
		case error:
			if err, ok := oops.AsOops(v); ok {
				return fmt.Sprintf("%+v", err), true
			}
		}

		return "", false
	})
	DefaultContext = setup.BeforeSuiteFn()
	DefaultContext.Logger.SetLogLevel(DefaultContext.Properties().String("log.level", "info"))
	DefaultContext.Infof("%s", DefaultContext.String())

	// TODO: add a system user to dummy fixtures
	api.SystemUserID = &dummy.JohnDoe.ID

	api.DefaultArtifactConnection = "connection://default/artifacts"

	if err := icrbac.Init(DefaultContext); err != nil {
		ginkgo.Fail(err.Error())
	}

	format.RegisterCustomFormatter(func(value interface{}) (string, bool) {
		switch value.(type) {

		case *models.PlaybookRun, models.PlaybookRun:
			s := ""
			actions, _ := value.(models.PlaybookRun).GetActions(DefaultContext.DB())
			for _, action := range actions {
				s += fmt.Sprintf("\t\t%s: %s %s\n", action.Name, action.Status, lo.FromPtrOr(action.Error, ""))
			}
			return s, true

		}

		return "", false
	})

	vars.AuthMode = ""
	metrics.RegisterDBStats(DefaultContext)
	e = echoSrv.New(DefaultContext)
	e.Use(auth.MockAuthMiddleware)

	events.StartConsumers(DefaultContext)
	if err := playbook.StartPlaybookConsumers(DefaultContext); err != nil {
		ginkgo.Fail(err.Error())
	}

	server = httptest.NewServer(e)
	metricsServer = httptest.NewServer(echoSrv.MetricsHandler())
	client = sdk.NewPlaybookClient(server.URL)
	client.Client = client.
		Auth(dummy.JohnDoe.Name, "admin").
		Header("X-Trace", "true")
	logger.Infof("Started test server @ %s", server.URL)
	logger.Infof("Started metrics server @ %s", metricsServer.URL)

	if err := testdata.LoadConnections(DefaultContext); err != nil {
		ginkgo.Fail(err.Error())
	}
	if err := testdata.LoadPlaybooks(DefaultContext); err != nil {
		ginkgo.Fail(err.Error())
	}
	if err := testdata.LoadRBAC(DefaultContext); err != nil {
		ginkgo.Fail(err.Error())
	}

	// artifactReader reads artifacts through their connections: built-in roles grant nothing
	reader := setup.CreateUserWithRole(DefaultContext, "E2E Artifact Reader", "e2e-artifact-reader@test.com")
	artifactReader = reader.ID.String()
	grantAccess("e2e-artifact-reader",
		v1.RoleBindingSubjects{PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{reader.Email}}},
		accessGrant{Action: policy.ActionRead, Resource: allConnections},
		accessGrant{Action: adapter.ActionConnectionUse, Resource: allConnections},
	)
})

var _ = ginkgo.AfterSuite(func() {
	setup.AfterSuiteFn()
	if server != nil {
		server.Close()
	}
	if metricsServer != nil {
		metricsServer.Close()
	}
	stopContainers()
})
