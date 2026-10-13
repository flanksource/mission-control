package permissions_test

import (
	"net/http"
	"net/http/httptest"

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = ginkgo.Describe("Lookup views of components, checks and playbooks", ginkgo.Ordered, func() {
	const namespace = "lookup-views"

	var (
		componentA, componentB models.Component
		canaryA, canaryB       models.Canary
		checkA, checkB         models.Check
		playbookA, playbookB   models.Playbook

		componentReader, checkReader, playbookReader, stranger, admin *models.Person
	)

	ginkgo.BeforeAll(func() {
		Expect(rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)).To(Succeed())

		componentA = models.Component{ID: uuid.New(), Name: "lookup-component-a", Type: "Lookup", Status: types.ComponentStatusHealthy, Labels: types.JSONStringMap{"lookup-a": "x"}}
		componentB = models.Component{ID: uuid.New(), Name: "lookup-component-b", Type: "Lookup", Status: types.ComponentStatusHealthy, Labels: types.JSONStringMap{"lookup-b": "x"}}
		canaryA = models.Canary{ID: uuid.New(), Name: "lookup-canary-a", Namespace: namespace, Spec: []byte("{}")}
		canaryB = models.Canary{ID: uuid.New(), Name: "lookup-canary-b", Namespace: namespace, Spec: []byte("{}")}
		checkA = models.Check{ID: uuid.New(), CanaryID: canaryA.ID, Name: "lookup-check-a", Type: "http", Status: "healthy", Labels: map[string]string{"lookup-a": "x"}}
		checkB = models.Check{ID: uuid.New(), CanaryID: canaryB.ID, Name: "lookup-check-b", Type: "http", Status: "healthy", Labels: map[string]string{"lookup-b": "x"}}
		playbookA = models.Playbook{ID: uuid.New(), Name: "lookup-playbook-a", Namespace: namespace, Source: models.SourceUI, Spec: []byte(`{"actions":[]}`)}
		playbookB = models.Playbook{ID: uuid.New(), Name: "lookup-playbook-b", Namespace: namespace, Source: models.SourceUI, Spec: []byte(`{"actions":[]}`)}

		// Scopes read stored membership, so the resources they select must exist before they're built
		for _, row := range []any{&componentA, &componentB, &canaryA, &canaryB, &checkA, &checkB, &playbookA, &playbookB} {
			Expect(DefaultContext.DB().Create(row).Error).To(Succeed())
		}

		ref := func(name string) v1.ScopeResourceRef { return v1.ScopeResourceRef{Name: name} }
		componentReader = setup.CreateUserWithRole(DefaultContext, "Lookup Component Reader", "lookup-component-reader@test.com", policy.RoleGuest)
		checkReader = setup.CreateUserWithRole(DefaultContext, "Lookup Check Reader", "lookup-check-reader@test.com", policy.RoleGuest)
		playbookReader = setup.CreateUserWithRole(DefaultContext, "Lookup Playbook Reader", "lookup-playbook-reader@test.com", policy.RoleGuest)
		stranger = setup.CreateUserWithRole(DefaultContext, "Lookup Stranger", "lookup-stranger@test.com", policy.RoleGuest)
		admin = setup.CreateUserWithRole(DefaultContext, "Lookup Admin", "lookup-admin@test.com", policy.RoleAdmin)

		for _, b := range []struct {
			name    string
			reader  *models.Person
			targets []v1.ScopeTarget
		}{
			{"components-a", componentReader, []v1.ScopeTarget{{Component: &v1.ScopeLabelledSelector{ScopeResourceRef: ref(componentA.Name)}}}},
			// check_summary joins the check's canary, so the reader reads that too
			{"checks-a", checkReader, []v1.ScopeTarget{
				{Check: &v1.ScopeLabelledSelector{ScopeResourceRef: ref(checkA.Name)}},
				{Canary: &v1.ScopeCanarySelector{ScopeResourceRef: ref(canaryA.Name)}},
			}},
			{"playbooks-a", playbookReader, []v1.ScopeTarget{{Playbook: &v1.ScopePlaybookRef{Name: playbookA.Name}}}},
		} {
			uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }
			meta := metav1.ObjectMeta{Name: b.name, Namespace: namespace}
			meta.UID = uid()
			Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{ObjectMeta: meta, Spec: v1.ScopeSpec{Targets: b.targets}})).To(Succeed())
			meta.UID = uid()
			Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{ObjectMeta: meta, Spec: v1.RoleSpec{Rules: []v1.RoleRule{
				{Name: "read", Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: b.name}},
			}}})).To(Succeed())
			meta.UID = uid()
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{ObjectMeta: meta, Spec: v1.RoleBindingSpec{
				Role: b.name, Subjects: boundPeople(b.reader),
			}})).To(Succeed())
		}
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	ginkgo.AfterAll(func() {
		deleteNamespace(namespace)
		for _, row := range []any{&playbookA, &playbookB, &checkA, &checkB, &canaryA, &canaryB, &componentA, &componentB} {
			Expect(DefaultContext.DB().Delete(row).Error).To(Succeed())
		}
		for _, p := range []*models.Person{componentReader, checkReader, playbookReader, stranger, admin} {
			Expect(DefaultContext.DB().Delete(p).Error).To(Succeed())
		}
	})

	listStatus := func(person *models.Person, view string) int {
		ginkgo.GinkgoHelper()
		req := httptest.NewRequest(http.MethodGet, "/db/"+view, nil)
		req = req.WithContext(DefaultContext.WithUser(person))
		rec := httptest.NewRecorder()
		handler := mcRBAC.DbMiddleware()(func(c echo.Context) error { return c.NoContent(http.StatusOK) })
		Expect(handler(echo.New().NewContext(req, rec))).To(Succeed())
		return rec.Code
	}

	// visible returns the values of the column the person reads in the view, of the two rows the test made.
	visible := func(person *models.Person, view, column string, values ...any) []string {
		ginkgo.GinkgoHelper()
		var got []string
		Expect(auth.WithRLS(DefaultContext.WithUser(person), func(ctx context.Context) error {
			return ctx.DB().Table(view).Where(column+" IN ?", values).Pluck(column, &got).Error
		})).To(Succeed())
		return got
	}

	type lookupView struct {
		name, column string
		a, b         any
	}

	// viewsByReader are the lookup views of each reader's type
	viewsByReader := func() map[*models.Person][]lookupView {
		return map[*models.Person][]lookupView{
			componentReader: {{"component_labels_keys", "key", "label:lookup-a", "label:lookup-b"}},
			checkReader: {
				{"checks_labels_keys", "key", "label:lookup-a", "label:lookup-b"},
				{"check_summary", "id", checkA.ID.String(), checkB.ID.String()},
			},
			playbookReader: {{"playbook_names", "id", playbookA.ID.String(), playbookB.ID.String()}},
		}
	}

	ginkgo.It("lets a Scope-only reader of each type read its lookup views, filtered to the rows it can read", func() {
		for reader, own := range viewsByReader() {
			for _, v := range own {
				Expect(listStatus(reader, v.name)).To(Equal(http.StatusOK), "%s reads %s", reader.Name, v.name)
				Expect(visible(reader, v.name, v.column, v.a, v.b)).To(ConsistOf(v.a), "%s sees only its own rows in %s", reader.Name, v.name)
				Expect(visible(admin, v.name, v.column, v.a, v.b)).To(ConsistOf(v.a, v.b), "a whole-type reader sees every row of %s", v.name)
			}
		}
	})

	ginkgo.It("refuses the lookup views of a type to a subject without a read grant on it", func() {
		views := viewsByReader()
		for owner, own := range views {
			for _, v := range own {
				for reader := range views {
					if reader != owner {
						Expect(listStatus(reader, v.name)).To(Equal(http.StatusForbidden), "%s has no grant on %s", reader.Name, v.name)
					}
				}
				Expect(listStatus(stranger, v.name)).To(Equal(http.StatusForbidden), "a guest without grants reads %s", v.name)
			}
		}
	})
})
