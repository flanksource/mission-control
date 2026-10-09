package permissions_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

// The X-Flanksource-Scope header limits the checks made through rbac.HasPermission by the same rule as listings:
// every resource of a check must be in at least one of its Scopes.
var _ = ginkgo.Describe("Scope impersonation of resource checks", ginkgo.Ordered, ginkgo.ContinueOnFailure, func() {
	var (
		e                   *echo.Echo
		admin, guest        *models.Person
		cfgIn, cfgOut       models.ConfigItem
		connIn, connOut     models.Connection
		runbook, approvable models.Playbook
		scopes              = map[string]string{}
	)

	ginkgo.BeforeAll(func() {
		Expect(rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)).To(Succeed())

		admin = setup.CreateUserWithRole(DefaultContext, "Impersonated Checks Admin", "impersonated-checks-admin@test.com", policy.RoleAdmin)
		guest = setup.CreateUserWithRole(DefaultContext, "Impersonated Checks Guest", "impersonated-checks-guest@test.com", policy.RoleGuest)

		for _, cfg := range []*models.ConfigItem{&cfgIn, &cfgOut} {
			side := lo.Ternary(cfg == &cfgIn, "in", "out")
			*cfg = models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("impersonated-checks-" + side),
				ConfigClass: "Pod",
				Type:        lo.ToPtr("Kubernetes::Pod"),
				Tags:        types.JSONStringMap{"impersonated-checks": side},
			}
			Expect(DefaultContext.DB().Create(cfg).Error).To(Succeed())
		}

		for _, conn := range []*models.Connection{&connIn, &connOut} {
			side := lo.Ternary(conn == &connIn, "in", "out")
			*conn = models.Connection{
				ID:        uuid.New(),
				Name:      "impersonated-checks-" + side,
				Namespace: "default",
				Type:      models.ConnectionTypeHTTP,
				URL:       "http://example.com",
				Source:    models.SourceUI,
			}
			Expect(DefaultContext.DB().Create(conn).Error).To(Succeed())
		}

		runbook = models.Playbook{
			ID:        uuid.New(),
			Name:      "impersonated-checks-run",
			Namespace: "default",
			Source:    models.SourceUI,
			Spec:      []byte(`{"configs":[{"name":"*"}],"actions":[{"name":"echo","exec":{"script":"echo"}}]}`),
		}
		approvable = models.Playbook{
			ID:        uuid.New(),
			Name:      "impersonated-checks-approve",
			Namespace: "default",
			Source:    models.SourceUI,
			Spec:      []byte(fmt.Sprintf(`{"configs":[{"name":"*"}],"approval":{"approvers":{"people":[%q]}},"actions":[{"name":"echo","exec":{"script":"echo"}}]}`, admin.Email)),
		}
		Expect(DefaultContext.DB().Create(&runbook).Error).To(Succeed())
		Expect(DefaultContext.DB().Create(&approvable).Error).To(Succeed())

		for name, targets := range map[string][]v1.ScopeTarget{
			"in": {
				{Config: &v1.ScopeConfigSelector{TagSelector: "impersonated-checks=in"}},
				{Connection: &v1.ScopeConnectionSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: connIn.Name}}},
			},
			"out":       {{Config: &v1.ScopeConfigSelector{TagSelector: "impersonated-checks=out"}}},
			"playbooks": {{Playbook: &v1.ScopePlaybookRef{Name: "impersonated-checks-*"}}},
		} {
			scope := &v1.Scope{
				ObjectMeta: metav1.ObjectMeta{Name: "impersonated-checks-" + name, Namespace: "default", UID: k8sTypes.UID(uuid.NewString())},
				Spec:       v1.ScopeSpec{Targets: targets},
			}
			Expect(db.PersistScopeFromCRD(DefaultContext, scope)).To(Succeed())
			scopes[name] = string(scope.UID)
		}

		// The guest reads configs through "in" only
		Expect(DefaultContext.DB().Create(&models.Permission{
			Name:           "impersonated-checks-guest",
			Namespace:      "default",
			Action:         policy.ActionRead,
			Subject:        guest.ID.String(),
			SubjectType:    models.PermissionSubjectTypePerson,
			ObjectSelector: []byte(`{"scopes":[{"namespace":"default","name":"impersonated-checks-in"}]}`),
		}).Error).To(Succeed())
		Expect(rbac.ReloadPolicy()).To(Succeed())

		e = echo.New()
		e.Use(auth.ScopeImpersonation)
		playbook.RegisterRoutes(e)

		// The read check views, plugins and others make on a single resource
		e.GET("/test/read-config/:id", func(c echo.Context) error {
			ctx := c.Request().Context().(context.Context)
			attr := &models.ABACAttribute{Config: models.ConfigItem{ID: uuid.MustParse(c.Param("id"))}}
			if !rbac.HasPermission(ctx, ctx.Subject(), attr, policy.ActionRead) {
				return c.NoContent(http.StatusForbidden)
			}
			return c.NoContent(http.StatusOK)
		})

		// Resolving a connection checks read on it
		e.GET("/test/connection/:name", func(c echo.Context) error {
			ctx := c.Request().Context().(context.Context)
			if _, err := connection.Get(ctx, "connection://default/"+c.Param("name")); err != nil {
				return c.String(http.StatusForbidden, err.Error())
			}
			return c.NoContent(http.StatusOK)
		})
	})

	ginkgo.AfterAll(func() {
		DefaultContext.DB().Where("playbook_id IN ?", []uuid.UUID{runbook.ID, approvable.ID}).Delete(&models.PlaybookApproval{})
		DefaultContext.DB().Where("playbook_id IN ?", []uuid.UUID{runbook.ID, approvable.ID}).Delete(&models.PlaybookRun{})
		DefaultContext.DB().Delete(&runbook)
		DefaultContext.DB().Delete(&approvable)
		DefaultContext.DB().Where("name = ?", "impersonated-checks-guest").Delete(&models.Permission{})
		DefaultContext.DB().Where("name LIKE ?", "impersonated-checks-%").Delete(&models.Scope{})
		DefaultContext.DB().Delete(&connIn)
		DefaultContext.DB().Delete(&connOut)
		DefaultContext.DB().Delete(&cfgIn)
		DefaultContext.DB().Delete(&cfgOut)
		DefaultContext.DB().Delete(admin)
		DefaultContext.DB().Delete(guest)
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	// do sends a request as the user, with the header naming the given Scope ids when any are given.
	do := func(user *models.Person, method, path string, body any, scopeIDs ...string) *httptest.ResponseRecorder {
		ginkgo.GinkgoHelper()

		var reader *bytes.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			Expect(err).ToNot(HaveOccurred())
			reader = bytes.NewReader(raw)
		} else {
			reader = bytes.NewReader(nil)
		}

		req := httptest.NewRequest(method, path, reader)
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req = req.WithContext(DefaultContext.WithUser(user))
		if scopeIDs != nil {
			header, err := json.Marshal(scopeIDs)
			Expect(err).ToNot(HaveOccurred())
			req.Header.Set(auth.HeaderFlanksourceScope, string(header))
		}

		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	run := func(configID uuid.UUID, scopeIDs ...string) *httptest.ResponseRecorder {
		ginkgo.GinkgoHelper()
		return do(admin, http.MethodPost, "/playbook/run", map[string]any{"id": runbook.ID, "config_id": configID}, scopeIDs...)
	}

	pendingRun := func(configID uuid.UUID) uuid.UUID {
		ginkgo.GinkgoHelper()
		r := models.PlaybookRun{
			ID:         uuid.New(),
			PlaybookID: approvable.ID,
			Status:     models.PlaybookRunStatusPendingApproval,
			Spec:       approvable.Spec,
			ConfigID:   &configID,
			CreatedBy:  &admin.ID,
		}
		Expect(DefaultContext.DB().Create(&r).Error).To(Succeed())
		return r.ID
	}

	ginkgo.Context("playbook:run", func() {
		ginkgo.It("is refused on a config outside the header's Scopes", func() {
			rec := run(cfgOut.ID, scopes["in"], scopes["playbooks"])
			Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())
		})

		ginkgo.It("is allowed when the playbook and the config are each in one of the header's Scopes", func() {
			rec := run(cfgIn.ID, scopes["in"], scopes["playbooks"])
			Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())
		})

		ginkgo.It("is refused when the playbook is in none of the header's Scopes", func() {
			rec := run(cfgIn.ID, scopes["in"])
			Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())
		})

		ginkgo.It("is unchanged without the header", func() {
			rec := run(cfgOut.ID)
			Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())
		})

		ginkgo.It("is allowed without a target when the playbook is in one of the header's Scopes", func() {
			rec := do(admin, http.MethodPost, "/playbook/run", map[string]any{"id": runbook.ID}, scopes["playbooks"])
			Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())
		})
	})

	ginkgo.Context("playbook:approve", func() {
		ginkgo.It("is refused on a run against a config outside the header's Scope", func() {
			rec := do(admin, http.MethodPost, "/playbook/run/approve/"+pendingRun(cfgOut.ID).String(), nil, scopes["in"], scopes["playbooks"])
			Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())
		})

		ginkgo.It("is allowed on a run against a config in the header's Scope", func() {
			rec := do(admin, http.MethodPost, "/playbook/run/approve/"+pendingRun(cfgIn.ID).String(), nil, scopes["in"], scopes["playbooks"])
			Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		})

		ginkgo.It("is unchanged without the header", func() {
			rec := do(admin, http.MethodPost, "/playbook/run/approve/"+pendingRun(cfgOut.ID).String(), nil)
			Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		})
	})

	ginkgo.Context("resolving a connection", func() {
		ginkgo.It("is refused outside the header's Scope", func() {
			rec := do(admin, http.MethodGet, "/test/connection/"+connOut.Name, nil, scopes["in"])
			Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())
		})

		ginkgo.It("is allowed in the header's Scope", func() {
			rec := do(admin, http.MethodGet, "/test/connection/"+connIn.Name, nil, scopes["in"])
			Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		})

		ginkgo.It("is refused when the header's Scope selects no connection", func() {
			rec := do(admin, http.MethodGet, "/test/connection/"+connIn.Name, nil, scopes["out"])
			Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())
		})

		ginkgo.It("is unchanged without the header", func() {
			rec := do(admin, http.MethodGet, "/test/connection/"+connOut.Name, nil)
			Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		})
	})

	ginkgo.Context("read on a config", func() {
		read := func(user *models.Person, cfg models.ConfigItem, scopeIDs ...string) int {
			ginkgo.GinkgoHelper()
			return do(user, http.MethodGet, "/test/read-config/"+cfg.ID.String(), nil, scopeIDs...).Code
		}

		ginkgo.It("is narrowed for a subject whose rules allow every config", func() {
			Expect(read(admin, cfgIn, scopes["in"])).To(Equal(http.StatusOK))
			Expect(read(admin, cfgOut, scopes["in"])).To(Equal(http.StatusForbidden))
			Expect(read(admin, cfgOut)).To(Equal(http.StatusOK))
		})

		ginkgo.It("allows a resource in any of the header's Scopes", func() {
			Expect(read(admin, cfgIn, scopes["in"], scopes["out"])).To(Equal(http.StatusOK))
			Expect(read(admin, cfgOut, scopes["in"], scopes["out"])).To(Equal(http.StatusOK))
		})

		ginkgo.It("allows nothing with an empty header, or one naming only a Scope that doesn't exist", func() {
			Expect(read(admin, cfgIn, []string{}...)).To(Equal(http.StatusForbidden), "header []")
			Expect(read(admin, cfgIn, uuid.NewString())).To(Equal(http.StatusForbidden), "a Scope that doesn't exist")
			Expect(read(admin, cfgIn, scopes["in"], uuid.NewString())).To(Equal(http.StatusOK), "a Scope that doesn't exist adds nothing")
		})

		ginkgo.It("can only narrow what the subject's grants allow", func() {
			Expect(read(guest, cfgIn)).To(Equal(http.StatusOK))
			Expect(read(guest, cfgIn, scopes["out"])).To(Equal(http.StatusForbidden))
			Expect(read(guest, cfgOut, scopes["out"])).To(Equal(http.StatusForbidden), "a Scope the guest has no grant through")
			Expect(read(guest, cfgOut, scopes["in"], scopes["out"])).To(Equal(http.StatusForbidden))
		})
	})
})
