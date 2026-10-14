package permissions_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
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

var _ = ginkgo.Describe("Artifact producer authorization", ginkgo.Ordered, func() {
	const namespace = "artifact-producers"
	const content = "producer artifact content"
	type artifactCase struct {
		list   string
		id     uuid.UUID
		reader string
	}
	var (
		e                             *echo.Echo
		adminLocal, adminRole, nobody *models.Person
		readers                       = map[string]*models.Person{}
		tokens                        = map[string]*models.Person{}
		artifactIDs                   []uuid.UUID
		check                         models.Check
		config                        models.ConfigItem
		change                        models.ConfigChange
		scraper                       models.ConfigScraper
		playbook                      models.Playbook
		runs                          []models.PlaybookRun
		cases                         []artifactCase
	)

	request := func(person *models.Person, path string) *httptest.ResponseRecorder {
		ginkgo.GinkgoHelper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(DefaultContext.WithUser(person))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	saveArtifact := func(a models.Artifact) uuid.UUID {
		ginkgo.GinkgoHelper()
		a.ID = uuid.New()
		a.Path = namespace + "/" + a.ID.String()
		a.Filename = "producer.txt"
		a.ContentType = "text/plain"
		Expect(a.SetContent([]byte(content), "gzip", 0)).To(Succeed())
		Expect(DefaultContext.DB().Create(&a).Error).To(Succeed())
		artifactIDs = append(artifactIDs, a.ID)
		return a.ID
	}

	grant := func(name string, person *models.Person, targets ...v1.ScopeTarget) {
		ginkgo.GinkgoHelper()
		Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec:       v1.ScopeSpec{Targets: targets},
		})).To(Succeed())
		Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec:       v1.RoleSpec{Rules: []v1.RoleRule{{Name: "read", Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: name}}}},
		})).To(Succeed())
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec:       v1.RoleBindingSpec{Role: name, Subjects: boundPeople(person)},
		})).To(Succeed())
	}

	ginkgo.BeforeAll(func() {
		Expect(mcRBAC.Init(DefaultContext)).To(Succeed())
		var err error
		adminLocal, err = auth.GetOrCreateAdminUser(DefaultContext)
		Expect(err).ToNot(HaveOccurred())
		Expect(shipped.AddToAdmins(DefaultContext, adminLocal.ID)).To(Succeed())
		Expect(shipped.Sync(DefaultContext)).To(Succeed())
		adminRole = setup.CreateUserWithRole(DefaultContext, "Artifact Admin Role", "artifact-admin-role@test.com")
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: namespace, Namespace: adapter.ShippedNamespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec:       v1.RoleBindingSpec{Role: shipped.RoleAdmin, Subjects: boundPeople(adminRole)},
		})).To(Succeed())
		nobody = setup.CreateUserWithRole(DefaultContext, "Artifact Nobody", "artifact-nobody@test.com")

		check = models.Check{ID: uuid.New(), CanaryID: dummy.LogisticsAPICanary.ID, Name: namespace, Type: "http"}
		config = models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr(namespace), Type: lo.ToPtr("Artifact::Config"), ConfigClass: "Config"}
		change = models.ConfigChange{ID: uuid.NewString(), ConfigID: config.ID.String(), ChangeType: "diff"}
		scraper = models.ConfigScraper{ID: uuid.New(), Name: namespace, Namespace: namespace, AgentID: dummy.HomelabAgent.ID, Spec: `{}`, Source: models.SourceUI}
		playbook = models.Playbook{ID: uuid.New(), Name: namespace, Namespace: namespace, Source: models.SourceUI, Spec: []byte(`{"actions":[]}`)}
		for _, record := range []any{&check, &config, &change, &scraper, &playbook} {
			Expect(DefaultContext.DB().Create(record).Error).To(Succeed())
		}

		for _, name := range []string{"check", "config", "scraper", "playbook", "run-config", "run-check"} {
			readers[name] = setup.CreateUserWithRole(DefaultContext, "Artifact "+name, "artifact-"+name+"@test.com")
		}
		checkTarget := v1.ScopeTarget{Check: &v1.ScopeLabelledSelector{ScopeResourceRef: v1.ScopeResourceRef{ID: check.ID.String()}}}
		configTarget := v1.ScopeTarget{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{ID: config.ID.String()}}}
		playbookTarget := v1.ScopeTarget{Playbook: &v1.ScopePlaybookRef{ID: playbook.ID.String()}}
		grant("check", readers["check"], checkTarget)
		grant("config", readers["config"], configTarget)
		grant("scraper", readers["scraper"], v1.ScopeTarget{Scraper: &v1.ScopeScraperSelector{ScopeResourceRef: v1.ScopeResourceRef{ID: scraper.ID.String()}}})
		grant("playbook", readers["playbook"], playbookTarget)
		grant("run-config", readers["run-config"], playbookTarget, configTarget)
		grant("run-check", readers["run-check"], playbookTarget, checkTarget)
		grant("outside", nobody, v1.ScopeTarget{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{ID: dummy.LogisticsAPI.ID.String()}}})

		checkTime := time.Now().UTC().Truncate(time.Second)
		changeID := uuid.MustParse(change.ID)
		cases = append(cases,
			artifactCase{"/artifacts/list/check/" + check.ID.String() + "/" + checkTime.Format(time.RFC3339), saveArtifact(models.Artifact{CheckID: &check.ID, CheckTime: &checkTime}), "check"},
			artifactCase{"/artifacts/list/config_change/" + change.ID, saveArtifact(models.Artifact{ConfigChangeID: &changeID}), "config"},
			artifactCase{"", saveArtifact(models.Artifact{ScraperID: &scraper.ID}), "scraper"},
		)
		for _, target := range []string{"playbook", "run-config", "run-check"} {
			run := models.PlaybookRun{ID: uuid.New(), PlaybookID: playbook.ID, Status: models.PlaybookRunStatusCompleted, Spec: []byte(`{}`), CreatedBy: &nobody.ID}
			if target == "run-config" {
				run.ConfigID = &config.ID
			}
			if target == "run-check" {
				run.CheckID = &check.ID
			}
			Expect(DefaultContext.DB().Create(&run).Error).To(Succeed())
			runs = append(runs, run)
			action := models.PlaybookRunAction{ID: uuid.New(), Name: "artifact", PlaybookRunID: run.ID, Status: models.PlaybookActionStatusCompleted}
			Expect(DefaultContext.DB().Create(&action).Error).To(Succeed())
			cases = append(cases, artifactCase{"/artifacts/list/playbook_run/" + run.ID.String(), saveArtifact(models.Artifact{PlaybookRunActionID: &action.ID}), target})
		}
		for name, reader := range readers {
			for _, role := range []string{"outside", name} {
				result, err := db.CreateAccessTokenForPerson(DefaultContext, reader, namespace+"-"+name+"-"+role, time.Hour, false,
					models.AccessTokenRoles{{Namespace: namespace, Name: role}})
				Expect(err).ToNot(HaveOccurred())
				_, err = rbac.Enforcer().AddGroupingPolicy(result.Person.ID.String(), reader.ID.String())
				Expect(err).ToNot(HaveOccurred())
				tokens[name+"-"+role] = result.Person
			}
		}
		Expect(rbac.ReloadPolicy()).To(Succeed())
		auth.FlushTokenCache()
		e = echo.New()
		artifacts.RegisterRoutes(e)
	})

	ginkgo.AfterAll(func() {
		Expect(DefaultContext.DB().Where("id IN ?", artifactIDs).Delete(&models.Artifact{}).Error).To(Succeed())
		for _, run := range runs {
			Expect(DefaultContext.DB().Where("playbook_run_id = ?", run.ID).Delete(&models.PlaybookRunAction{}).Error).To(Succeed())
			Expect(DefaultContext.DB().Delete(&run).Error).To(Succeed())
		}
		for _, record := range []any{&change, &config, &check, &scraper, &playbook} {
			Expect(DefaultContext.DB().Delete(record).Error).To(Succeed())
		}
		Expect(DefaultContext.DB().Where("namespace = ? OR (namespace = ? AND name = ?)", namespace, adapter.ShippedNamespace, namespace).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{}).Error).To(Succeed())
		for _, person := range tokens {
			Expect(DefaultContext.DB().Where("person_id = ?", person.ID).Delete(&models.AccessToken{}).Error).To(Succeed())
			Expect(DefaultContext.DB().Delete(person).Error).To(Succeed())
		}
		for _, person := range append(lo.Values(readers), adminRole, nobody) {
			Expect(DefaultContext.DB().Delete(person).Error).To(Succeed())
		}
		Expect(rbac.ReloadPolicy()).To(Succeed())
		auth.FlushTokenCache()
	})

	for _, subject := range []string{"admins", "admin Role", "producer reader", "no producer read", "limited token", "covered token"} {
		ginkgo.It("serves producer artifacts to "+subject+" only when readable", func() {
			for _, tc := range cases {
				person, allowed := readers[tc.reader], true
				switch subject {
				case "admins":
					person = adminLocal
				case "admin Role":
					person = adminRole
				case "no producer read":
					person, allowed = nobody, false
				case "limited token":
					person, allowed = tokens[tc.reader+"-outside"], false
				case "covered token":
					person = tokens[tc.reader+"-"+tc.reader]
				}
				paths := []string{"/artifacts/download/" + tc.id.String()}
				if tc.list != "" {
					paths = append(paths, tc.list)
				}
				for _, path := range paths {
					rec := request(person, path)
					if !allowed {
						Expect(rec.Code).To(Equal(http.StatusNotFound), "%s: %s", path, rec.Body.String())
						continue
					}
					Expect(rec.Code).To(Equal(http.StatusOK), "%s: %s", path, rec.Body.String())
					if path == tc.list {
						var listed []models.Artifact
						Expect(json.Unmarshal(rec.Body.Bytes(), &listed)).To(Succeed())
						Expect(listed).To(HaveLen(1))
						Expect(listed[0].ID).To(Equal(tc.id))
					} else {
						Expect(rec.Body.String()).To(Equal(content))
					}
				}
			}
		})
	}

	ginkgo.It("requires both the playbook and the run's target, regardless of who started it", func() {
		for _, tc := range cases {
			if tc.reader != "run-config" && tc.reader != "run-check" {
				continue
			}
			targetReader := readers["config"]
			if tc.reader == "run-check" {
				targetReader = readers["check"]
			}
			for _, person := range []*models.Person{readers["playbook"], targetReader, nobody} {
				Expect(request(person, tc.list).Code).To(Equal(http.StatusNotFound))
				Expect(request(person, "/artifacts/download/"+tc.id.String()).Code).To(Equal(http.StatusNotFound))
			}
		}
	})

	ginkgo.It("returns the same response for missing, unreadable and unowned artifacts or producers", func() {
		unowned := saveArtifact(models.Artifact{})
		for _, tc := range cases {
			missing := request(adminLocal, "/artifacts/download/"+uuid.NewString())
			for _, rec := range []*httptest.ResponseRecorder{
				request(nobody, "/artifacts/download/"+tc.id.String()),
				request(adminLocal, "/artifacts/download/"+unowned.String()),
			} {
				Expect(rec.Code).To(Equal(http.StatusNotFound))
				Expect(rec.Body.String()).To(Equal(missing.Body.String()))
			}
		}
		for _, path := range []string{
			"/artifacts/list/check/" + uuid.NewString() + "/2026-01-01T00:00:00Z",
			"/artifacts/list/config_change/" + uuid.NewString(),
			"/artifacts/list/playbook_run/" + uuid.NewString(),
		} {
			rec := request(adminLocal, path)
			Expect(rec.Code).To(Equal(http.StatusNotFound), rec.Body.String())
			Expect(rec.Body.String()).To(Equal(request(nobody, cases[0].list).Body.String()))
		}
	})

	ginkgo.It("refuses artifacts whose producer has been deleted", func() {
		Expect(DefaultContext.DB().Model(&check).Update("deleted_at", time.Now()).Error).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(DefaultContext.DB().Model(&check).Update("deleted_at", nil).Error).To(Succeed())
		})
		missing := request(adminLocal, "/artifacts/download/"+uuid.NewString())
		for _, path := range []string{cases[0].list, "/artifacts/download/" + cases[0].id.String()} {
			rec := request(adminLocal, path)
			Expect(rec.Code).To(Equal(http.StatusNotFound))
			Expect(rec.Body.String()).To(Equal(missing.Body.String()))
		}
	})
})
