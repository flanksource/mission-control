package permissions_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/rbac/adapter"

	mcRBAC "github.com/flanksource/incident-commander/rbac"
)

var _ = Describe("Scope, Role and RoleBinding API", Ordered, func() {
	const namespace = "rbac-api"

	var (
		e            *echo.Echo
		admin, guest *models.Person
	)

	request := func(person *models.Person, method, path string, body any) *httptest.ResponseRecorder {
		GinkgoHelper()
		var payload []byte
		if body != nil {
			var err error
			payload, err = json.Marshal(body)
			Expect(err).ToNot(HaveOccurred())
		}

		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req = req.WithContext(DefaultContext.WithUser(person))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	decode := func(rec *httptest.ResponseRecorder, into any) {
		GinkgoHelper()
		Expect(json.Unmarshal(rec.Body.Bytes(), into)).To(Succeed(), rec.Body.String())
	}

	object := func(name string, spec any) map[string]any {
		return map[string]any{"metadata": map[string]any{"name": name, "namespace": namespace}, "spec": spec}
	}

	BeforeAll(func() {
		Expect(rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)).To(Succeed())

		admin = setup.CreateUserWithRole(DefaultContext, "RBAC API Admin", "rbac-api-admin@test.com", policy.RoleAdmin)
		guest = setup.CreateUserWithRole(DefaultContext, "RBAC API Guest", "rbac-api-guest@test.com", policy.RoleGuest)

		e = echo.New()
		mcRBAC.RegisterRoutes(e)
		e.Any("/db/*", func(c echo.Context) error { return c.NoContent(http.StatusOK) }, mcRBAC.DbMiddleware())
	})

	AfterAll(func() {
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(admin).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(guest).Error).To(Succeed())
	})

	It("creates a scope", func() {
		rec := request(admin, http.MethodPost, "/rbac/scopes", object("all-playbooks", map[string]any{
			"targets": []any{map[string]any{"playbook": map[string]any{"name": "*"}}},
		}))
		Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())

		var scope models.Scope
		decode(rec, &scope)
		Expect(scope.Source).To(Equal(models.SourceUI))
		Expect(scope.CreatedBy).To(Equal(&admin.ID))
		Expect(scope.Error).To(BeNil())
	})

	It("refuses to create an object that exists", func() {
		rec := request(admin, http.MethodPost, "/rbac/scopes", object("all-playbooks", map[string]any{
			"targets": []any{map[string]any{"playbook": map[string]any{"name": "*"}}},
		}))
		Expect(rec.Code).To(Equal(http.StatusConflict), rec.Body.String())
	})

	It("stores an invalid role, and responds with why it isn't in effect", func() {
		rec := request(admin, http.MethodPost, "/rbac/roles", object("runner", map[string]any{
			"rules": []any{map[string]any{"name": "run", "action": policy.ActionPlaybookRun, "resource": map[string]any{"scopeRef": "missing"}}},
		}))
		Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())

		var role models.Role
		decode(rec, &role)
		Expect(role.ErrorReason).To(HaveValue(Equal(adapter.ReasonScopeNotFound)))
	})

	rejected := []struct {
		name, path string
		body       map[string]any
	}{
		{"a scope with two types in a target", "/rbac/scopes", object("two-types", map[string]any{
			"targets": []any{map[string]any{"config": map[string]any{"name": "*"}, "playbook": map[string]any{"name": "*"}}},
		})},
		{"a scope with more than ten targets", "/rbac/scopes", object("too-many", map[string]any{
			"targets": lo.Times(11, func(int) any { return map[string]any{"playbook": map[string]any{"name": "*"}} }),
		})},
		{"a scope with an unknown resource type", "/rbac/scopes", object("unknown-type", map[string]any{
			"targets": []any{map[string]any{"playbooks": map[string]any{"name": "*"}}},
		})},
		{"a scope with an unknown selector field", "/rbac/scopes", object("unknown-field", map[string]any{
			"targets": []any{map[string]any{"playbook": map[string]any{"name": "*", "owner": "me"}}},
		})},
		{"a role named after a built-in role", "/rbac/roles", object(policy.RoleViewer, map[string]any{
			"rules": []any{map[string]any{"name": "run", "action": policy.ActionPlaybookRun, "resource": map[string]any{"scopeRef": "all-playbooks"}}},
		})},
		{"a role with an action pattern", "/rbac/roles", object("pattern", map[string]any{
			"rules": []any{map[string]any{"name": "run", "action": "playbook:*", "resource": map[string]any{"scopeRef": "all-playbooks"}}},
		})},
		{"a role denying reads", "/rbac/roles", object("deny-read", map[string]any{
			"rules": []any{map[string]any{"name": "read", "action": policy.ActionRead, "deny": true, "resource": map[string]any{"scopeRef": "all-playbooks"}}},
		})},
		{"a role with a target on an action that takes none", "/rbac/roles", object("mcp-target", map[string]any{
			"rules": []any{map[string]any{"name": "mcp", "action": policy.ActionMCPRun, "resource": map[string]any{"scopeRef": "all-playbooks"}, "target": map[string]any{"scopeRef": "all-playbooks"}}},
		})},
		{"a binding without subjects", "/rbac/role-bindings", object("no-subjects", map[string]any{
			"role": "runner", "subjects": map[string]any{},
		})},
		{"a binding to admins", "/rbac/role-bindings", object("admins", map[string]any{
			"role": "runner", "subjects": map[string]any{"roles": []string{policy.RoleAdmin}},
		})},
		{"a binding to viewers", "/rbac/role-bindings", object("viewers", map[string]any{
			"role": "runner", "subjects": map[string]any{"roles": []string{policy.RoleViewer}},
		})},
		{"a binding with a match that doesn't compile", "/rbac/role-bindings", object("bad-match", map[string]any{
			"role": "runner", "subjects": map[string]any{"oidc": []any{map[string]any{"provider": "oipa", "match": "claims.tenant =="}}},
		})},
	}

	for _, tt := range rejected {
		It("rejects "+tt.name+", storing nothing", func() {
			rec := request(admin, http.MethodPost, tt.path, tt.body)
			Expect(rec.Code).To(Equal(http.StatusBadRequest), rec.Body.String())

			metadata := tt.body["metadata"].(map[string]any)
			Expect(request(admin, http.MethodGet, tt.path+"/"+namespace+"/"+metadata["name"].(string), nil).Code).To(Equal(http.StatusNotFound))
		})
	}

	It("stores a binding whose role doesn't exist, and responds with why it isn't in effect", func() {
		rec := request(admin, http.MethodPost, "/rbac/role-bindings", object("orphan", map[string]any{
			"role": "missing", "subjects": map[string]any{"roles": []string{policy.RoleEveryone}},
		}))
		Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())

		var binding models.RoleBinding
		decode(rec, &binding)
		Expect(binding.ErrorReason).To(HaveValue(Equal(adapter.ReasonRoleNotFound)))

		Expect(request(admin, http.MethodDelete, "/rbac/role-bindings/"+namespace+"/orphan", nil).Code).To(Equal(http.StatusOK))
	})

	It("updates a role, keeping who created it", func() {
		rec := request(admin, http.MethodPut, "/rbac/roles/"+namespace+"/runner", map[string]any{"spec": map[string]any{
			"rules": []any{map[string]any{"name": "run", "action": policy.ActionPlaybookRun, "resource": map[string]any{"scopeRef": "all-playbooks"}}},
		}})
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())

		var role models.Role
		decode(rec, &role)
		Expect(role.Error).To(BeNil())
		Expect(role.CreatedBy).To(Equal(&admin.ID))
	})

	It("refuses to rename an object on update", func() {
		rec := request(admin, http.MethodPut, "/rbac/roles/"+namespace+"/runner", object("renamed", map[string]any{"rules": []any{}}))
		Expect(rec.Code).To(Equal(http.StatusBadRequest), rec.Body.String())
	})

	It("creates a role binding, and gets and lists it", func() {
		rec := request(admin, http.MethodPost, "/rbac/role-bindings", object("runners", map[string]any{
			"role":     "runner",
			"subjects": map[string]any{"people": []string{guest.Email}},
		}))
		Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())

		rec = request(admin, http.MethodGet, "/rbac/role-bindings/"+namespace+"/runners", nil)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())

		rec = request(admin, http.MethodGet, "/rbac/role-bindings?namespace="+namespace, nil)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		var bindings []models.RoleBinding
		decode(rec, &bindings)
		Expect(bindings).To(HaveLen(1))
	})

	It("refuses to change or delete an object managed by Kubernetes", func() {
		crd := models.Scope{ID: uuid.New(), Name: "from-kubernetes", Namespace: namespace, Source: models.SourceCRD, Targets: []byte(`[{"config":{"name":"*"}}]`)}
		Expect(DefaultContext.DB().Create(&crd).Error).To(Succeed())

		rec := request(admin, http.MethodPut, "/rbac/scopes/"+namespace+"/from-kubernetes", map[string]any{"spec": map[string]any{
			"targets": []any{map[string]any{"config": map[string]any{"name": "*"}}},
		}})
		Expect(rec.Code).To(Equal(http.StatusConflict), rec.Body.String())

		rec = request(admin, http.MethodDelete, "/rbac/scopes/"+namespace+"/from-kubernetes", nil)
		Expect(rec.Code).To(Equal(http.StatusConflict), rec.Body.String())
	})

	It("deletes an object", func() {
		rec := request(admin, http.MethodDelete, "/rbac/role-bindings/"+namespace+"/runners", nil)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())

		rec = request(admin, http.MethodGet, "/rbac/role-bindings/"+namespace+"/runners", nil)
		Expect(rec.Code).To(Equal(http.StatusNotFound), rec.Body.String())
	})

	It("requires update on rbac to write, and read on rbac to read", func() {
		rec := request(guest, http.MethodPost, "/rbac/scopes", object("by-guest", map[string]any{
			"targets": []any{map[string]any{"config": map[string]any{"name": "*"}}},
		}))
		Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())

		rec = request(guest, http.MethodGet, "/rbac/scopes", nil)
		Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())
	})

	It("refuses writes through /db, even from admins", func() {
		for _, table := range []string{"scopes", "roles", "role_bindings"} {
			rec := request(admin, http.MethodPost, "/db/"+table, map[string]any{"name": "bypass"})
			Expect(rec.Code).To(Equal(http.StatusMethodNotAllowed), table)

			rec = request(admin, http.MethodGet, "/db/"+table, nil)
			Expect(rec.Code).To(Equal(http.StatusOK), table)

			rec = request(guest, http.MethodGet, "/db/"+table, nil)
			Expect(rec.Code).To(Equal(http.StatusForbidden), table)
		}
	})
})
