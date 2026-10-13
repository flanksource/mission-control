package permissions_test

import (
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/artifacts"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/rbac/shipped"
)

var _ = ginkgo.Describe("Scraper Scopes", ginkgo.Ordered, func() {
	const namespace = "scraper-scopes"

	var (
		homelabA, homelabB, gcp                          models.ConfigScraper
		gcpArtifact, homelabArtifact                     uuid.UUID
		homelabReader, everyReader, configReader, nobody *models.Person
		builtinAdmin, builtinEditor, adminsMember        *models.Person
		scopeIDs                                         = map[string]string{}
	)

	scraperIDs := func() []uuid.UUID { return []uuid.UUID{homelabA.ID, homelabB.ID, gcp.ID} }

	newScraper := func(name string, agent models.Agent) models.ConfigScraper {
		ginkgo.GinkgoHelper()
		scraper := models.ConfigScraper{ID: uuid.New(), Name: name, Namespace: namespace, AgentID: agent.ID, Spec: `{}`, Source: models.SourceUI}
		Expect(DefaultContext.DB().Create(&scraper).Error).To(Succeed())
		return scraper
	}

	newArtifact := func(scraper models.ConfigScraper) uuid.UUID {
		ginkgo.GinkgoHelper()
		id := uuid.New()
		Expect(DefaultContext.DB().Exec(`INSERT INTO artifacts (id, scraper_id, path, filename, size, content_type, checksum)
			VALUES (?, ?, 'scraper-scopes', 'scraper-scopes.txt', 0, 'text/plain', '')`, id, scraper.ID).Error).To(Succeed())
		return id
	}

	newScope := func(name string, targets ...v1.ScopeTarget) {
		ginkgo.GinkgoHelper()
		scope := &v1.Scope{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec:       v1.ScopeSpec{Targets: targets},
		}
		Expect(db.PersistScopeFromCRD(DefaultContext, scope)).To(Succeed())
		scopeIDs[name] = string(scope.UID)
	}

	newRole := func(name, scope string) {
		ginkgo.GinkgoHelper()
		role := &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec: v1.RoleSpec{Rules: []v1.RoleRule{
				{Name: "read", Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: scope}},
			}},
		}
		Expect(db.PersistRoleFromCRD(DefaultContext, role)).To(Succeed())
	}

	bind := func(role string, person *models.Person) {
		ginkgo.GinkgoHelper()
		binding := &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: role, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec: v1.RoleBindingSpec{Role: role, Subjects: v1.RoleBindingSubjects{
				PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{person.Email}},
			}},
		}
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).To(Succeed())
	}

	newPermission := func(person *models.Person, action, object, scope string) {
		ginkgo.GinkgoHelper()
		permission := models.Permission{
			ID:          uuid.New(),
			Name:        "scraper-scopes-" + uuid.NewString(),
			Namespace:   namespace,
			Action:      action,
			Object:      object,
			Subject:     person.ID.String(),
			SubjectType: models.PermissionSubjectTypePerson,
		}
		if scope != "" {
			permission.ObjectSelector = []byte(`{"scopes":[{"namespace":"` + namespace + `","name":"` + scope + `"}]}`)
		}
		Expect(DefaultContext.DB().Create(&permission).Error).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(DefaultContext.DB().Delete(&permission).Error).To(Succeed())
			Expect(rbac.ReloadPolicy()).To(Succeed())
			auth.FlushTokenCache()
		})
		Expect(rbac.ReloadPolicy()).To(Succeed())
		auth.FlushTokenCache()
	}

	dbStatusOf := func(person *models.Person, method, table string) int {
		ginkgo.GinkgoHelper()
		req := httptest.NewRequest(method, "/db/"+table, nil)
		req = req.WithContext(DefaultContext.WithUser(person))
		rec := httptest.NewRecorder()
		handler := mcRBAC.DbMiddleware()(func(c echo.Context) error { return c.NoContent(http.StatusOK) })
		Expect(handler(echo.New().NewContext(req, rec))).To(Succeed())
		return rec.Code
	}

	dbStatus := func(person *models.Person, method string) int {
		ginkgo.GinkgoHelper()
		return dbStatusOf(person, method, "config_scrapers")
	}

	payload := func(person *models.Person) *rls.Payload {
		ginkgo.GinkgoHelper()
		p, err := auth.GetRLSPayload(DefaultContext.WithUser(person))
		Expect(err).ToNot(HaveOccurred())
		return p
	}

	// list returns the status of listing config_scrapers through /db, and the test's scrapers and scraper artifacts
	// Postgres returns under the person's row filters, which PostgREST applies through the same claim.
	list := func(person *models.Person) (int, []uuid.UUID, []uuid.UUID) {
		ginkgo.GinkgoHelper()

		req := httptest.NewRequest(http.MethodGet, "/db/config_scrapers", nil)
		req = req.WithContext(DefaultContext.WithUser(person))
		rec := httptest.NewRecorder()
		handler := mcRBAC.DbMiddleware()(func(c echo.Context) error { return c.NoContent(http.StatusOK) })
		Expect(handler(echo.New().NewContext(req, rec))).To(Succeed())

		var scrapers, artifacts []uuid.UUID
		Expect(auth.WithRLS(DefaultContext.WithUser(person), func(ctx context.Context) error {
			if err := ctx.DB().Model(&models.ConfigScraper{}).Where("id IN ?", scraperIDs()).Pluck("id", &scrapers).Error; err != nil {
				return err
			}
			return ctx.DB().Table("artifacts").Where("id IN ?", []uuid.UUID{gcpArtifact, homelabArtifact}).Pluck("id", &artifacts).Error
		})).To(Succeed())
		return rec.Code, scrapers, artifacts
	}

	ginkgo.BeforeAll(func() {
		Expect(rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)).To(Succeed())

		homelabA = newScraper("scraper-scopes-homelab-a", dummy.HomelabAgent)
		homelabB = newScraper("scraper-scopes-homelab-b", dummy.HomelabAgent)
		gcp = newScraper("scraper-scopes-gcp", dummy.GCPAgent)
		homelabArtifact = newArtifact(homelabA)
		gcpArtifact = newArtifact(gcp)

		newScope("homelab-scrapers", v1.ScopeTarget{Scraper: &v1.ScopeScraperSelector{Agent: dummy.HomelabAgent.Name}})
		newScope("every-scraper", v1.ScopeTarget{Scraper: &v1.ScopeScraperSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}})
		newScope("every-config", v1.ScopeTarget{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}})
		newRole("read-homelab-scrapers", "homelab-scrapers")
		newRole("read-every-scraper", "every-scraper")
		newRole("read-every-config", "every-config")

		homelabReader = setup.CreateUserWithRole(DefaultContext, "Scraper Homelab Reader", "scraper-homelab@test.com", policy.RoleGuest)
		everyReader = setup.CreateUserWithRole(DefaultContext, "Scraper Every Reader", "scraper-every@test.com", policy.RoleGuest)
		configReader = setup.CreateUserWithRole(DefaultContext, "Scraper Config Reader", "scraper-config@test.com", policy.RoleGuest)
		nobody = setup.CreateUserWithRole(DefaultContext, "Scraper Nobody", "scraper-nobody@test.com", policy.RoleGuest)
		builtinAdmin = setup.CreateUserWithRole(DefaultContext, "Scraper Admin", "scraper-admin@test.com", policy.RoleAdmin)
		builtinEditor = setup.CreateUserWithRole(DefaultContext, "Scraper Editor", "scraper-editor@test.com", policy.RoleEditor)
		adminsMember = setup.CreateUserWithRole(DefaultContext, "Scraper Admins Member", "scraper-admins-member@test.com", policy.RoleGuest)

		bind("read-homelab-scrapers", homelabReader)
		bind("read-every-scraper", everyReader)
		bind("read-every-config", configReader)

		Expect(shipped.AddToAdmins(DefaultContext, adminsMember.ID)).To(Succeed())
		Expect(shipped.Sync(DefaultContext)).To(Succeed())
		Expect(rbac.ReloadPolicy()).To(Succeed())
		auth.FlushTokenCache()
	})

	ginkgo.AfterAll(func() {
		Expect(DefaultContext.DB().Exec(`DELETE FROM team_members WHERE team_id IN (SELECT id FROM teams WHERE source = ?)`, adapter.SourceShipped).Error).To(Succeed())
		for _, ns := range []string{namespace, adapter.ShippedNamespace} {
			Expect(DefaultContext.DB().Where("namespace = ?", ns).Delete(&models.RoleBinding{}).Error).To(Succeed())
			Expect(DefaultContext.DB().Where("namespace = ?", ns).Delete(&models.Role{}).Error).To(Succeed())
			Expect(DefaultContext.DB().Where("namespace = ?", ns).Delete(&models.Scope{}).Error).To(Succeed())
		}
		Expect(DefaultContext.DB().Where("source = ?", adapter.SourceShipped).Delete(&models.Team{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Exec(`DELETE FROM artifacts WHERE id IN ?`, []uuid.UUID{gcpArtifact, homelabArtifact}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("id IN ?", scraperIDs()).Delete(&models.ConfigScraper{}).Error).To(Succeed())
		for _, p := range []*models.Person{homelabReader, everyReader, configReader, nobody, builtinAdmin, builtinEditor, adminsMember} {
			Expect(DefaultContext.DB().Delete(p).Error).To(Succeed())
		}
		Expect(rbac.ReloadPolicy()).To(Succeed())
		auth.FlushTokenCache()
	})

	ginkgo.It("stores a scraper target by agent id", func() {
		var rows []map[string]any
		Expect(DefaultContext.DB().Raw(`SELECT resource_type, agent_id::text AS agent_id, resource_id, name, name_prefix
			FROM scope_targets WHERE scope_id = ?`, scopeIDs["homelab-scrapers"]).Scan(&rows).Error).To(Succeed())
		Expect(rows).To(HaveLen(1))
		Expect(rows[0]).To(HaveKeyWithValue("resource_type", policy.ResourceScraper))
		Expect(rows[0]).To(HaveKeyWithValue("agent_id", dummy.HomelabAgent.ID.String()))
		Expect(rows[0]["resource_id"]).To(BeNil())
		Expect(rows[0]["name"]).To(BeNil())
	})

	ginkgo.It("lists only the scrapers of a Scope of one agent's scrapers, and only their artifacts", func() {
		Expect(payload(homelabReader).Scraper).To(Equal(&rls.Grants{Any: []rls.Grant{{Scope: scopeIDs["homelab-scrapers"]}}}))

		status, scrapers, artifacts := list(homelabReader)
		Expect(status).To(Equal(http.StatusOK))
		Expect(scrapers).To(ConsistOf(homelabA.ID, homelabB.ID))
		Expect(artifacts).To(Equal([]uuid.UUID{homelabArtifact}))

		var withStatus []uuid.UUID
		Expect(auth.WithRLS(DefaultContext.WithUser(homelabReader), func(ctx context.Context) error {
			return ctx.DB().Table("config_scrapers_with_status").Where("id IN ?", scraperIDs()).Pluck("id", &withStatus).Error
		})).To(Succeed())
		Expect(withStatus).To(ConsistOf(homelabA.ID, homelabB.ID))
	})

	ginkgo.It("lists every scraper through a whole-type read, naming its Scope so the claim follows Scope changes", func() {
		grant := &rls.Grants{Any: []rls.Grant{{Scope: scopeIDs["every-scraper"]}}}
		Expect(payload(everyReader).Scraper).To(Equal(grant))
		Expect(payload(everyReader).JWTClaims()).To(HaveKeyWithValue("scraper", grant))

		status, scrapers, artifacts := list(everyReader)
		Expect(status).To(Equal(http.StatusOK))
		Expect(scrapers).To(ConsistOf(scraperIDs()))
		Expect(artifacts).To(ConsistOf(homelabArtifact, gcpArtifact))
	})

	for _, tt := range []struct {
		name   string
		person func() *models.Person
	}{
		{"with no grant at all", func() *models.Person { return nobody }},
		{"with read on every config only", func() *models.Person { return configReader }},
	} {
		ginkgo.It("lists no scraper for a subject "+tt.name, func() {
			Expect(payload(tt.person()).Scraper).To(BeNil())

			status, scrapers, artifacts := list(tt.person())
			Expect(status).To(Equal(http.StatusForbidden))
			Expect(scrapers).To(BeEmpty())
			Expect(artifacts).To(BeEmpty())
		})
	}

	ginkgo.It("lists every scraper for built-in admins and editors, whose rows aren't filtered", func() {
		for _, person := range []*models.Person{builtinAdmin, builtinEditor} {
			Expect(payload(person).Disable).To(BeTrue(), person.Email)

			_, scrapers, artifacts := list(person)
			Expect(scrapers).To(ConsistOf(scraperIDs()), person.Email)
			Expect(artifacts).To(ConsistOf(homelabArtifact, gcpArtifact), person.Email)
		}

		status, _, _ := list(builtinAdmin)
		Expect(status).To(Equal(http.StatusOK))
	})

	ginkgo.It("lists every scraper for members of the shipped admins team, through the admin Role", func() {
		Expect(payload(adminsMember).Disable).To(BeFalse(), "a guest's rows are filtered")
		Expect(payload(adminsMember).Scraper).To(Equal(rls.AllRows()), "all-scrapers is shipped, so it never changes")
		Expect(payload(adminsMember).JWTClaims()).To(HaveKeyWithValue("scraper", rls.AllRows()))

		status, scrapers, artifacts := list(adminsMember)
		Expect(status).To(Equal(http.StatusOK))
		Expect(scrapers).To(ConsistOf(scraperIDs()))
		Expect(artifacts).To(ConsistOf(homelabArtifact, gcpArtifact))
	})

	ginkgo.It("lets a Permission naming a Scope of configs and scrapers filter scraper rows, but match no scraper in checks", func() {
		newScope("configs-and-homelab-scrapers",
			v1.ScopeTarget{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}},
			v1.ScopeTarget{Scraper: &v1.ScopeScraperSelector{Agent: dummy.HomelabAgent.Name}})
		permission := models.Permission{
			ID:             uuid.New(),
			Name:           "scraper-scopes-permission",
			Namespace:      namespace,
			Action:         policy.ActionRead,
			Subject:        nobody.ID.String(),
			SubjectType:    models.PermissionSubjectTypePerson,
			ObjectSelector: []byte(`{"scopes":[{"namespace":"` + namespace + `","name":"configs-and-homelab-scrapers"}]}`),
		}
		Expect(DefaultContext.DB().Create(&permission).Error).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(DefaultContext.DB().Delete(&permission).Error).To(Succeed())
			Expect(rbac.ReloadPolicy()).To(Succeed())
			auth.FlushTokenCache()
		})
		Expect(rbac.ReloadPolicy()).To(Succeed())
		auth.FlushTokenCache()

		conditions, err := adapter.PermissionScopeConditions(DefaultContext, nil, permission)
		Expect(err).ToNot(HaveOccurred())
		Expect(conditions).To(ConsistOf("'scope:"+scopeIDs["configs-and-homelab-scrapers"]+"' in r.obj.Membership.Config", "false"))

		Expect(payload(nobody).Scraper).To(Equal(&rls.Grants{Any: []rls.Grant{{Scope: scopeIDs["configs-and-homelab-scrapers"]}}}))
		_, scrapers, _ := list(nobody)
		Expect(scrapers).To(ConsistOf(homelabA.ID, homelabB.ID))
	})

	ginkgo.It("keeps a Permission naming only scrapers in effect for listings", func() {
		newPermission(nobody, policy.ActionRead, "", "homelab-scrapers")

		conditions, err := adapter.PermissionScopeConditions(DefaultContext, nil, models.Permission{
			ObjectSelector: []byte(`{"scopes":[{"namespace":"` + namespace + `","name":"homelab-scrapers"}]}`),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(conditions).To(Equal([]string{"false"}), "matches no single-resource check")

		Expect(payload(nobody).Scraper).To(Equal(&rls.Grants{Any: []rls.Grant{{Scope: scopeIDs["homelab-scrapers"]}}}))
		_, scrapers, artifacts := list(nobody)
		Expect(scrapers).To(ConsistOf(homelabA.ID, homelabB.ID))
		Expect(artifacts).To(Equal([]uuid.UUID{homelabArtifact}))
	})

	ginkgo.Describe("writes through /db", func() {
		var writer *models.Person

		ginkgo.BeforeAll(func() {
			writer = setup.CreateUserWithRole(DefaultContext, "Scraper Writer", "scraper-writer@test.com", policy.RoleGuest)
			role := &v1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: "write-every-scraper", Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
				Spec: v1.RoleSpec{Rules: []v1.RoleRule{
					{Name: "read", Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: "every-scraper"}},
					{Name: "update", Action: policy.ActionUpdate, Resource: &v1.ScopeReference{ScopeRef: "every-scraper"}},
				}},
			}
			Expect(db.PersistRoleFromCRD(DefaultContext, role)).To(Succeed())
			bind("write-every-scraper", writer)
			Expect(rbac.ReloadPolicy()).To(Succeed())
			auth.FlushTokenCache()
			ginkgo.DeferCleanup(func() { Expect(DefaultContext.DB().Delete(writer).Error).To(Succeed()) })
		})

		ginkgo.It("allows a whole-type update rule on scrapers", func() {
			Expect(dbStatus(writer, http.MethodPatch)).To(Equal(http.StatusOK))
			Expect(dbStatus(writer, http.MethodDelete)).To(Equal(http.StatusForbidden))
		})

		ginkgo.It("lets a token whose Roles allow scrapers use its owner's write Permission on database.config_scrapers", func() {
			newPermission(everyReader, policy.ActionUpdate, policy.ObjectDatabaseSettings, "")
			Expect(dbStatus(everyReader, http.MethodPatch)).To(Equal(http.StatusOK))

			token := func(role string) *models.Person {
				ginkgo.GinkgoHelper()
				result, err := db.CreateAccessTokenForPerson(DefaultContext, everyReader, "scraper-scopes-"+role, time.Hour, false,
					models.AccessTokenRoles{{Namespace: namespace, Name: role}})
				Expect(err).ToNot(HaveOccurred())
				_, err = rbac.Enforcer().AddGroupingPolicy(result.Person.ID.String(), everyReader.ID.String())
				Expect(err).ToNot(HaveOccurred())
				ginkgo.DeferCleanup(func() {
					Expect(DefaultContext.DB().Where("person_id = ?", result.Person.ID).Delete(&models.AccessToken{}).Error).To(Succeed())
				})
				return result.Person
			}
			writes, reads := token("write-every-scraper"), token("read-every-scraper")
			Expect(rbac.ReloadPolicy()).To(Succeed())

			Expect(dbStatus(writes, http.MethodPatch)).To(Equal(http.StatusOK))
			Expect(dbStatus(reads, http.MethodPatch)).To(Equal(http.StatusForbidden), "its Roles don't allow updating scrapers")
		})

	})

	ginkgo.Describe("artifact endpoints", func() {
		var e *echo.Echo

		ginkgo.BeforeAll(func() {
			e = echo.New()
			artifacts.RegisterRoutes(e)
		})

		ginkgo.It("only finds the artifacts of scrapers the caller can list", func() {
			for id, visible := range map[uuid.UUID]bool{homelabArtifact: true, gcpArtifact: false} {
				Expect(auth.CanReadArtifact(DefaultContext.WithUser(homelabReader), id)).To(Equal(visible))
				Expect(auth.CanReadArtifact(DefaultContext.WithUser(builtinAdmin), id)).To(BeTrue())
			}
		})

		ginkgo.It("doesn't download an artifact of a scraper the caller can't list", func() {
			newPermission(homelabReader, policy.ActionRead, policy.ObjectArtifact, "")

			req := httptest.NewRequest(http.MethodGet, "/artifacts/download/"+gcpArtifact.String(), nil)
			req = req.WithContext(DefaultContext.WithUser(homelabReader))
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			Expect(rec.Code).To(Equal(http.StatusNotFound), rec.Body.String())
		})
	})
})
