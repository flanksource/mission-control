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

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = Describe("Legacy grants of whole-type routes", Ordered, func() {
	const namespace = "whole-type-legacy"

	var (
		e                                                   *echo.Echo
		guest, viewer, editor, deniedEditor, silenceCreator *models.Person
		scopeWriter                                         *models.Person
		persons                                             []*models.Person
	)

	person := func(name, role string) *models.Person {
		GinkgoHelper()
		p := setup.CreateUserWithRole(DefaultContext, "Legacy "+name, "whole-type-legacy-"+name+"@test.com", role)
		persons = append(persons, p)
		return p
	}

	// token creates an access token of the owner limited by the Roles of the namespace, and returns the token's person
	token := func(owner *models.Person, name string, roles ...string) *models.Person {
		GinkgoHelper()
		tokenRoles := lo.Map(roles, func(role string, _ int) models.AccessTokenRole {
			return models.AccessTokenRole{Namespace: namespace, Name: role}
		})
		body, err := json.Marshal(auth.CreateTokenRequest{Name: name, Roles: tokenRoles})
		Expect(err).ToNot(HaveOccurred())
		req := httptest.NewRequest(http.MethodPost, "/auth/create_token", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req = req.WithContext(DefaultContext.WithUser(owner))
		rec := httptest.NewRecorder()
		Expect(auth.CreateToken(echo.New().NewContext(req, rec))).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())

		var accessToken models.AccessToken
		Expect(DefaultContext.DB().Where("name = ? AND created_by = ?", name, owner.ID).First(&accessToken).Error).To(Succeed())
		var p models.Person
		Expect(DefaultContext.DB().Where("id = ?", accessToken.PersonID).First(&p).Error).To(Succeed())
		persons = append(persons, &p)
		return &p
	}

	status := func(p *models.Person, method, path string) int {
		GinkgoHelper()
		return serveAs(e, p, method, path)
	}

	BeforeAll(func() {
		Expect(rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)).To(Succeed())

		guest = person("guest", policy.RoleGuest)
		viewer = person("viewer", policy.RoleViewer)
		editor = person("editor", policy.RoleEditor)
		deniedEditor = person("denied-editor", policy.RoleEditor)
		silenceCreator = person("silence-creator", policy.RoleGuest)
		scopeWriter = person("scope-writer", policy.RoleGuest)

		// A binding may select subjects by built-in role. What it grants them isn't the built-in role's own grant.
		guests := v1.RoleBindingSubjects{Roles: []string{policy.RoleGuest}}
		bindWholeType(namespace, "guests-notifications", v1.ResourceNotification, false, guests,
			policy.ActionRead, policy.ActionCreate, policy.ActionUpdate)
		bindWholeType(namespace, "guests-configs", policy.ResourceConfig, false, guests, policy.ActionRead)

		bindWholeType(namespace, "deny-silences", v1.ResourceNotificationSilence, true, boundPeople(deniedEditor),
			policy.ActionCreate, policy.ActionUpdate, policy.ActionDelete)
		bindWholeType(namespace, "create-silences", v1.ResourceNotificationSilence, false, boundPeople(silenceCreator),
			policy.ActionCreate)
		bindWholeType(namespace, "write-scopes", v1.ResourceScope, false, boundPeople(scopeWriter),
			policy.ActionCreate, policy.ActionUpdate, policy.ActionDelete)

		Expect(rbac.ReloadPolicy()).To(Succeed())
		e = wholeTypeRoutes()
	})

	AfterAll(func() {
		deleteNamespace(namespace)
		ids := lo.Map(persons, func(p *models.Person, _ int) uuid.UUID { return p.ID })
		Expect(DefaultContext.DB().Where("person_id IN ? OR created_by IN ?", ids, ids).Delete(&models.AccessToken{}).Error).To(Succeed())
		for _, p := range persons {
			_, err := rbac.Enforcer().DeletePermissionsForUser(p.ID.String())
			Expect(err).ToNot(HaveOccurred())
			Expect(DefaultContext.DB().Delete(p).Error).To(Succeed())
		}
	})

	It("doesn't count what a RoleBinding grants a built-in role as the role's own grant", func() {
		Expect(authorized(status(guest, http.MethodGet, "/db/notifications"))).To(BeTrue(), "the binding applies")

		Expect(status(guest, http.MethodPost, "/notification/silence")).To(Equal(http.StatusForbidden))
		Expect(status(guest, http.MethodGet, "/notification/silence_preview")).To(Equal(http.StatusForbidden))
		Expect(status(guest, http.MethodPatch, "/db/notification_silences")).To(Equal(http.StatusForbidden))
		Expect(status(guest, http.MethodGet, "/application/default/missing")).To(Equal(http.StatusForbidden))
	})

	It("doesn't let a built-in role override a deny of the type", func() {
		Expect(authorized(status(editor, http.MethodPost, "/notification/silence"))).To(BeTrue())

		Expect(status(deniedEditor, http.MethodPost, "/notification/silence")).To(Equal(http.StatusForbidden))
		Expect(status(deniedEditor, http.MethodPatch, "/db/notification_silences")).To(Equal(http.StatusForbidden))
		Expect(status(deniedEditor, http.MethodDelete, "/db/notification_silences")).To(Equal(http.StatusForbidden))
		Expect(authorized(status(deniedEditor, http.MethodGet, "/notification/silence_preview"))).To(BeTrue(), "read isn't denied")
	})

	It("grants each action on a type on its own", func() {
		Expect(authorized(status(silenceCreator, http.MethodPost, "/notification/silence"))).To(BeTrue())
		Expect(status(silenceCreator, http.MethodPatch, "/db/notification_silences")).To(Equal(http.StatusForbidden))
		Expect(status(silenceCreator, http.MethodDelete, "/db/notification_silences")).To(Equal(http.StatusForbidden))
		Expect(status(silenceCreator, http.MethodGet, "/notification/silence_preview")).To(Equal(http.StatusForbidden))
	})

	It("limits a token by its Roles on the type's own object, not on the legacy object", func() {
		Expect(authorized(status(editor, http.MethodPost, "/notification/silence"))).To(BeTrue())
		Expect(status(token(editor, "notifications", "guests-notifications"), http.MethodPost, "/notification/silence")).
			To(Equal(http.StatusForbidden), "a Role granting notifications doesn't grant their silences")
		Expect(authorized(status(token(editor, "silences", "create-silences"), http.MethodPost, "/notification/silence"))).To(BeTrue())

		Expect(authorized(status(viewer, http.MethodGet, "/application/default/missing"))).To(BeTrue())
		Expect(status(token(viewer, "configs", "guests-configs"), http.MethodGet, "/application/default/missing")).
			To(Equal(http.StatusForbidden), "a Role granting configs doesn't grant applications")

		Expect(status(token(silenceCreator, "configs", "guests-configs"), http.MethodPost, "/notification/silence")).To(Equal(http.StatusForbidden))
	})

	It("refuses writes to a Scope's targets through /db", func() {
		Expect(authorized(status(scopeWriter, http.MethodPost, "/rbac/scopes"))).To(BeTrue())
		for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
			Expect(status(scopeWriter, method, "/db/scope_targets")).To(Equal(http.StatusMethodNotAllowed), method)
		}
	})
})

var _ = Describe("Denies of whole-type objects bound to everyone", Ordered, func() {
	const namespace = "whole-type-everyone-denies"

	var (
		e      *echo.Echo
		editor *models.Person
	)

	BeforeAll(func() {
		Expect(rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)).To(Succeed())

		// The editor isn't in everyone until their first check adds them
		editor = setup.CreateUserWithRole(DefaultContext, "Everyone Deny Editor", "whole-type-everyone-deny@test.com", policy.RoleEditor)
		bindWholeType(namespace, "deny-silences", v1.ResourceNotificationSilence, true,
			v1.RoleBindingSubjects{Roles: []string{policy.RoleEveryone}}, policy.ActionCreate)

		Expect(rbac.ReloadPolicy()).To(Succeed())
		e = wholeTypeRoutes()
	})

	AfterAll(func() {
		deleteNamespace(namespace)
		_, err := rbac.Enforcer().DeleteRolesForUser(editor.ID.String())
		Expect(err).ToNot(HaveOccurred())
		Expect(DefaultContext.DB().Delete(editor).Error).To(Succeed())
	})

	It("doesn't let a built-in role override it, from the first request", func() {
		Expect(serveAs(e, editor, http.MethodPost, "/notification/silence")).To(Equal(http.StatusForbidden))
		Expect(serveAs(e, editor, http.MethodPost, "/notification/silence")).To(Equal(http.StatusForbidden))
	})
})
