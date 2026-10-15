package permissions_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/playbook"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
)

var _ = ginkgo.Describe("Playbook list", ginkgo.Ordered, func() {
	var (
		e     *echo.Echo
		admin *models.Person
		other models.Playbook
	)

	ginkgo.BeforeAll(func() {
		Expect(mcRBAC.Init(DefaultContext)).To(Succeed())

		admin = newAdmin("Playbook List Admin", "playbook-list-admin@test.com")

		other = models.Playbook{
			ID:        uuid.New(),
			Name:      "playbook-list-other",
			Namespace: "mc",
			Source:    models.SourceUI,
			Spec:      []byte(`{"configs":[{"name":"*"}],"actions":[{"name":"echo","exec":{"script":"echo"}}]}`),
		}
		Expect(DefaultContext.DB().Create(&other).Error).To(Succeed())

		e = echo.New()
		playbook.RegisterRoutes(e)
	})

	ginkgo.AfterAll(func() {
		Expect(DefaultContext.DB().Delete(&other).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(admin).Error).To(Succeed())
	})

	list := func() (int, []string) {
		ginkgo.GinkgoHelper()

		req := httptest.NewRequest(http.MethodGet, "/playbook/list?config_id="+dummy.NginxIngressPod.ID.String(), nil)
		req = req.WithContext(DefaultContext.WithUser(admin))

		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			return rec.Code, nil
		}

		var playbooks []api.PlaybookListItem
		Expect(json.Unmarshal(rec.Body.Bytes(), &playbooks)).To(Succeed())
		return rec.Code, lo.Map(playbooks, func(p api.PlaybookListItem, _ int) string { return p.Name })
	}

	ginkgo.It("lists every playbook of the config for a caller whose rows aren't filtered", func() {
		status, names := list()
		Expect(status).To(Equal(http.StatusOK))
		Expect(names).To(ContainElements(dummy.EchoConfig.Name, other.Name))
	})

	ginkgo.Describe("opening a run", func() {
		var run models.PlaybookRun

		ginkgo.BeforeAll(func() {
			run = models.PlaybookRun{ID: uuid.New(), PlaybookID: other.ID, Status: models.PlaybookRunStatusCompleted, Spec: other.Spec}
			Expect(DefaultContext.DB().Create(&run).Error).To(Succeed())
			ginkgo.DeferCleanup(func() { Expect(DefaultContext.DB().Delete(&run).Error).To(Succeed()) })
		})

		get := func(path string) int {
			ginkgo.GinkgoHelper()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req = req.WithContext(DefaultContext.WithUser(admin))
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			return rec.Code
		}

		ginkgo.It("returns a run of a playbook the caller may read", func() {
			Expect(get("/playbook/run/" + run.ID.String())).To(Equal(http.StatusOK))
		})
	})

	ginkgo.Describe("a subject without built-in access", func() {
		listStatus := func(readGrantsCoverPlaybooks bool) int {
			ginkgo.GinkgoHelper()

			readGrantsCover := mcRBAC.ReadGrantsCover
			mcRBAC.ReadGrantsCover = func(_ context.Context, resourceType string) bool {
				return readGrantsCoverPlaybooks && resourceType == policy.ResourcePlaybook
			}
			ginkgo.DeferCleanup(func() { mcRBAC.ReadGrantsCover = readGrantsCover })

			subject := models.FederatedPrincipal(admin.ID.String())
			req := httptest.NewRequest(http.MethodGet, "/playbook/list", nil)
			req = req.WithContext(DefaultContext.WithUser(admin).WithSubject(subject))
			rec := httptest.NewRecorder()
			handler := mcRBAC.PlaybookRead()(func(c echo.Context) error { return c.NoContent(http.StatusOK) })
			Expect(handler(echo.New().NewContext(req, rec))).To(Succeed())
			return rec.Code
		}

		ginkgo.It("lists playbooks when its read grants cover some of them", func() {
			Expect(listStatus(true)).To(Equal(http.StatusOK))
		})

		ginkgo.It("is denied when its read grants cover none", func() {
			Expect(listStatus(false)).To(Equal(http.StatusForbidden))
		})
	})
})
