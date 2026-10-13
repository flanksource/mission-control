package permissions_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/connection"
	"github.com/flanksource/incident-commander/db"
	mcpServer "github.com/flanksource/incident-commander/mcp"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/rbac/rbactest"
)

var _ = Describe("Reading and using a connection", Ordered, func() {
	const (
		namespace = "connection-use-test"
		password  = "connection-use-password"
	)

	var (
		conn                         models.Connection
		guest, reader, user, editor  *models.Person
		admin                        *models.Person
		previousDefaultLLMConnection string
		e                            *echo.Echo
	)

	uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }

	bindRole := func(name, action, scope string, person *models.Person) {
		GinkgoHelper()
		Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
			Spec: v1.RoleSpec{Rules: []v1.RoleRule{
				{Name: name, Action: action, Resource: &v1.ScopeReference{ScopeRef: scope}},
			}},
		})).To(Succeed())
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
			Spec: v1.RoleBindingSpec{Role: name, Subjects: v1.RoleBindingSubjects{
				PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{person.Email}},
			}},
		})).To(Succeed())
	}

	do := func(person *models.Person, method, path string) *httptest.ResponseRecorder {
		GinkgoHelper()
		req := httptest.NewRequest(method, path, nil)
		req = req.WithContext(DefaultContext.WithUser(person))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	BeforeAll(func() {
		Expect(mcRBAC.Init(DefaultContext)).To(Succeed())

		conn = models.Connection{
			ID:        uuid.New(),
			Name:      "connection-use",
			Namespace: namespace,
			Source:    models.SourceUI,
			Type:      models.ConnectionTypeFolder,
			Username:  "connection-use-user",
			Password:  password,
			URL:       "http://connection-use-user:" + password + "@example.com/$(username)",
			Properties: types.JSONStringMap{
				"path":   GinkgoT().TempDir(),
				"bearer": "connection-use-bearer",
				"region": "eu-west-1",
			},
		}
		Expect(DefaultContext.DB().Create(&conn).Error).To(Succeed())

		guest = setup.CreateUserWithRole(DefaultContext, "Connection Guest", "connection-use-guest@test.com")
		reader = setup.CreateUserWithRole(DefaultContext, "Connection Reader", "connection-use-reader@test.com")
		user = setup.CreateUserWithRole(DefaultContext, "Connection User", "connection-use-user@test.com")
		editor = setup.CreateUserWithRole(DefaultContext, "Connection Editor", "connection-use-editor@test.com")
		admin = setup.CreateUserWithRole(DefaultContext, "Connection Admin", "connection-use-admin@test.com")
		Expect(rbactest.MakeAdmins(DefaultContext, admin.ID)).To(Succeed())

		// Checks read stored membership, so the connection must exist before the Scopes are built.
		// Reads of connections can only select them all; connection:use selects the one connection.
		for name, selector := range map[string]v1.ScopeResourceRef{
			"all-connections": {Name: "*"},
			"test-connection": {Name: conn.Name, Namespace: namespace},
		} {
			Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
				Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Connection: &v1.ScopeConnectionSelector{ScopeResourceRef: selector}}}},
			})).To(Succeed())
		}
		bindRole("read-connections", policy.ActionRead, "all-connections", reader)
		bindRole("use-connection", adapter.ActionConnectionUse, "test-connection", user)

		// The editor reads and writes connections, but can't use them: what the built-in editor role granted
		Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: "edit-connections", Namespace: namespace, UID: uid()},
			Spec: v1.RoleSpec{Rules: lo.Map([]string{policy.ActionRead, policy.ActionCreate, policy.ActionUpdate, policy.ActionDelete}, func(action string, _ int) v1.RoleRule {
				return v1.RoleRule{Name: action, Action: action, Resource: &v1.ScopeReference{ScopeRef: "all-connections"}}
			})},
		})).To(Succeed())
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "edit-connections", Namespace: namespace, UID: uid()},
			Spec: v1.RoleBindingSpec{Role: "edit-connections", Subjects: v1.RoleBindingSubjects{
				PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{editor.Email}},
			}},
		})).To(Succeed())
		Expect(rbac.ReloadPolicy()).To(Succeed())

		previousDefaultLLMConnection = api.DefaultLLMConnection
		api.DefaultLLMConnection = fmt.Sprintf("connection://%s/%s", namespace, conn.Name)

		e = echo.New()
		connection.RegisterRoutes(e)
		e.Any("/db/*", func(c echo.Context) error { return c.NoContent(http.StatusOK) }, mcRBAC.DbMiddleware())
	})

	AfterAll(func() {
		api.DefaultLLMConnection = previousDefaultLLMConnection
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(&conn).Error).To(Succeed())
		for _, p := range []*models.Person{guest, reader, user, editor, admin} {
			Expect(DefaultContext.DB().Delete(p).Error).To(Succeed())
		}
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	Describe("read", func() {
		expectMasked := func(c models.Connection) {
			GinkgoHelper()
			Expect(c.Name).To(Equal(conn.Name))
			Expect(c.Type).To(Equal(conn.Type))
			Expect(c.Username).To(Equal(db.MaskedValue))
			Expect(c.Password).ToNot(ContainSubstring(password))
			Expect(c.URL).To(Equal("http://connection-use-user:***@example.com/$(username)"))
			Expect(c.Properties).To(HaveKeyWithValue("bearer", db.MaskedValue))
			Expect(c.Properties).To(HaveKeyWithValue("region", "eu-west-1"))
		}

		It("masks credentials, but keeps the secrets they reference", func() {
			masked := db.MaskConnection(models.Connection{
				Username:    "secret://creds/username",
				Password:    password,
				Certificate: "-----BEGIN CERTIFICATE-----",
				Properties:  types.JSONStringMap{"clientSecret": "configmap://oauth/secret", "session_token": "plain"},
			})
			Expect(masked.Username).To(Equal("secret://creds/username"))
			Expect(masked.Password).To(Equal(db.MaskedValue))
			Expect(masked.Certificate).To(Equal(db.MaskedValue))
			Expect(masked.Properties).To(HaveKeyWithValue("clientSecret", "configmap://oauth/secret"))
			Expect(masked.Properties).To(HaveKeyWithValue("session_token", db.MaskedValue))
		})

		It("is reading connection_details through /db", func() {
			for _, p := range []*models.Person{reader, editor, admin} {
				Expect(do(p, http.MethodGet, "/db/connection_details").Code).To(Equal(http.StatusOK), p.Email)
			}
			Expect(do(guest, http.MethodGet, "/db/connection_details").Code).To(Equal(http.StatusForbidden))
			Expect(do(user, http.MethodGet, "/db/connection_details").Code).To(Equal(http.StatusForbidden), "connection:use alone doesn't read")
		})

		It("masks credentials in the MCP list_connections tool", func() {
			conns, err := db.ListConnections(DefaultContext)
			Expect(err).ToNot(HaveOccurred())
			var found bool
			for _, c := range conns {
				if c.ID == conn.ID {
					found = true
					expectMasked(c)
				}
			}
			Expect(found).To(BeTrue())
		})

		It("masks credentials in the MCP connection resource", func() {
			var req mcp.ReadResourceRequest
			req.Params.URI = fmt.Sprintf("connection://%s/%s", namespace, conn.Name)
			contents, err := mcpServer.ConnectionResourceHandler(DefaultContext.WithUser(admin), req)
			Expect(err).ToNot(HaveOccurred())
			Expect(contents).To(HaveLen(1))

			text := contents[0].(mcp.TextResourceContents).Text
			Expect(text).ToNot(ContainSubstring(password))
			Expect(text).ToNot(ContainSubstring("connection-use-bearer"))

			var c models.Connection
			Expect(json.Unmarshal([]byte(text), &c)).To(Succeed())
			expectMasked(c)
		})
	})

	for _, tt := range []struct{ name, method, path string }{
		{"testing a connection", http.MethodPost, "/connection/test/%s"},
		{"getting the default LLM connection", http.MethodGet, "/connection/llm"},
	} {
		Describe(tt.name, func() {
			path := func() string {
				if tt.method == http.MethodPost {
					return fmt.Sprintf(tt.path, conn.ID)
				}
				return tt.path
			}

			It("is refused without connection:use", func() {
				Expect(do(guest, tt.method, path()).Code).To(Equal(http.StatusForbidden))
			})

			It("is refused with read alone", func() {
				Expect(rbac.HasPermission(DefaultContext, reader.ID.String(), &models.ABACAttribute{Connection: conn}, policy.ActionRead)).To(BeTrue())
				Expect(do(reader, tt.method, path()).Code).To(Equal(http.StatusForbidden))
			})

			It("is allowed with connection:use", func() {
				rec := do(user, tt.method, path())
				Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
			})

			It("is allowed for a member of the shipped admins team", func() {
				rec := do(admin, tt.method, path())
				Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
			})

			It("is refused with update on connections, which transitionally allowed it", func() {
				Expect(mcRBAC.CheckContext(DefaultContext.WithUser(editor), policy.ObjectConnection, policy.ActionUpdate)).To(BeTrue())
				Expect(do(editor, tt.method, path()).Code).To(Equal(http.StatusForbidden))
			})

			It("is refused when the request is limited to Scopes the connection isn't in", func() {
				req := httptest.NewRequest(tt.method, path(), nil)
				req = req.WithContext(rbac.LimitToScopes(DefaultContext.WithUser(admin), []string{uuid.NewString()}))
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, req)
				Expect(rec.Code).To(Equal(http.StatusForbidden))
			})

		})
	}

	Describe("writing", func() {
		send := func(person *models.Person, method, path string, body any) *httptest.ResponseRecorder {
			GinkgoHelper()
			data, err := json.Marshal(body)
			Expect(err).ToNot(HaveOccurred())
			req := httptest.NewRequest(method, path, bytes.NewReader(data))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			req = req.WithContext(DefaultContext.WithUser(person))
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			return rec
		}

		It("returns a connection masked to readers", func() {
			Expect(do(guest, http.MethodGet, "/connection/"+conn.ID.String()).Code).To(Equal(http.StatusForbidden))

			rec := do(editor, http.MethodGet, "/connection/"+conn.ID.String())
			Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
			Expect(rec.Body.String()).ToNot(ContainSubstring(password))
			Expect(rec.Body.String()).ToNot(ContainSubstring("connection-use-bearer"))
		})

		It("keeps credentials sent back masked, and changes the rest", func() {
			rec := do(editor, http.MethodGet, "/connection/"+conn.ID.String())
			Expect(rec.Code).To(Equal(http.StatusOK))
			var masked models.Connection
			Expect(json.Unmarshal(rec.Body.Bytes(), &masked)).To(Succeed())
			masked.Properties["region"] = "us-east-1"

			Expect(send(reader, http.MethodPut, "/connection/"+conn.ID.String(), masked).Code).To(Equal(http.StatusForbidden))
			rec = send(editor, http.MethodPut, "/connection/"+conn.ID.String(), masked)
			Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())

			stored, err := db.GetConnection(DefaultContext, conn.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(stored.Username).To(Equal(conn.Username))
			Expect(stored.Password).To(Equal(password))
			Expect(stored.URL).To(Equal(conn.URL))
			Expect(stored.Properties).To(HaveKeyWithValue("bearer", "connection-use-bearer"))
			Expect(stored.Properties).To(HaveKeyWithValue("region", "us-east-1"))
		})

		It("creates and deletes a connection with create and delete on connections", func() {
			body := models.Connection{Name: "connection-write", Namespace: namespace, Type: models.ConnectionTypeHTTP, Password: "write-password"}
			Expect(send(reader, http.MethodPost, "/connection", body).Code).To(Equal(http.StatusForbidden))

			rec := send(editor, http.MethodPost, "/connection", body)
			Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())
			Expect(rec.Body.String()).ToNot(ContainSubstring("write-password"))
			var created models.Connection
			Expect(json.Unmarshal(rec.Body.Bytes(), &created)).To(Succeed())

			Expect(send(editor, http.MethodPost, "/connection", body).Code).To(Equal(http.StatusConflict))

			stored, err := db.GetConnection(DefaultContext, created.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(stored.Password).To(Equal("write-password"))
			Expect(stored.Source).To(Equal(models.SourceUI))
			Expect(stored.CreatedBy).To(Equal(&editor.ID))

			Expect(do(reader, http.MethodDelete, "/connection/"+created.ID.String()).Code).To(Equal(http.StatusForbidden))
			Expect(do(editor, http.MethodDelete, "/connection/"+created.ID.String()).Code).To(Equal(http.StatusNoContent))
			Expect(do(editor, http.MethodGet, "/connection/"+created.ID.String()).Code).To(Equal(http.StatusNotFound))
			Expect(DefaultContext.DB().Delete(&models.Connection{}, "id = ?", created.ID).Error).To(Succeed())
		})
	})

	It("returns the hydrated default LLM connection to those who may use it", func() {
		rec := do(user, http.MethodGet, "/connection/llm")
		Expect(rec.Code).To(Equal(http.StatusOK))

		var c models.Connection
		Expect(json.Unmarshal(rec.Body.Bytes(), &c)).To(Succeed())
		Expect(c.Password).To(Equal(password))
	})
})
