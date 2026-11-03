package permissions_test

import (
	"fmt"
	"net/http"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/auth"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
)

var _ = Describe("Tables whose reads /db leaves to RLS", Ordered, func() {
	var (
		e        *echo.Echo
		viewer   *models.Person
		writer   *models.Person
		view     models.View
		panel    models.ViewPanel
		artifact models.Artifact
		// statement is the SQL PostgREST runs for the request being served
		statement string
		args      []any
		affected  int64
	)

	BeforeAll(func() {
		Expect(mcRBAC.Init(DefaultContext)).To(Succeed())
		viewer = newViewer("RLS-only Viewer", "rls-only-viewer@test.com")
		writer = setup.CreateUserWithRole(DefaultContext, "RLS-only Writer", "rls-only-writer@test.com")

		view = models.View{ID: uuid.New(), Name: "rls-only", Namespace: "default", Spec: types.JSON(`{"display":{"title":"RLS only"}}`)}
		Expect(DefaultContext.DB().Create(&view).Error).To(Succeed())
		panel = models.ViewPanel{ViewID: view.ID, AgentID: uuid.Nil, Results: types.JSON(`[{"name":"original"}]`)}
		Expect(DefaultContext.DB().Create(&panel).Error).To(Succeed())
		artifact = models.Artifact{ID: uuid.New(), CheckID: &dummy.LogisticsAPIHealthHTTPCheck.ID, CheckTime: &time.Time{},
			Path: "rls-only/" + uuid.NewString(), Filename: "rls-only.txt", ContentType: "text/plain"}
		Expect(artifact.SetContent([]byte("content"), "gzip", 0)).To(Succeed())
		Expect(DefaultContext.DB().Create(&artifact).Error).To(Succeed())

		// The handler stands in for PostgREST: it runs the request's SQL as postgrest_api with the subject's claims.
		e = echo.New()
		e.Any("/db/*", func(c echo.Context) error {
			ctx := c.Request().Context().(context.Context)
			payload, err := auth.GetRLSPayload(ctx)
			if err != nil {
				return err
			}
			return ctx.DB().Transaction(func(tx *gorm.DB) error {
				if err := payload.SetPostgresSessionRLS(tx); err != nil {
					return err
				}
				result := tx.Exec(statement, args...)
				affected = result.RowsAffected
				if result.Error != nil {
					return result.Error
				}
				return c.NoContent(http.StatusOK)
			})
		}, mcRBAC.DbMiddleware())
	})

	AfterAll(func() {
		Expect(DefaultContext.DB().Where("id = ?", artifact.ID).Delete(&models.Artifact{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("view_id = ?", view.ID).Delete(&models.ViewPanel{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("id = ?", view.ID).Delete(&models.View{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(viewer).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(writer).Error).To(Succeed())
	})

	serveAsPerson := func(person *models.Person, method, path, sql string, sqlArgs ...any) string {
		GinkgoHelper()
		statement, args, affected = sql, sqlArgs, 0
		return fmt.Sprintf("%d, %d rows", serveAs(e, person, method, path), affected)
	}
	serve := func(method, path, sql string, sqlArgs ...any) string {
		GinkgoHelper()
		return serveAsPerson(viewer, method, path, sql, sqlArgs...)
	}

	It("lets a viewer read them, filtered by RLS", func() {
		Expect(map[string]string{
			"views_summary":    serve(http.MethodGet, "/db/views_summary", "SELECT * FROM views_summary WHERE id = ?", view.ID),
			"view_panels":      serve(http.MethodGet, "/db/view_panels", "SELECT * FROM view_panels WHERE view_id = ?", view.ID),
			"artifacts":        serve(http.MethodGet, "/db/artifacts", "SELECT * FROM artifacts WHERE id = ?", artifact.ID),
			"artifact_summary": serve(http.MethodGet, "/db/artifact_summary", "SELECT * FROM artifact_summary LIMIT 0"),
		}).To(Equal(map[string]string{
			"views_summary":    "200, 1 rows",
			"view_panels":      "200, 1 rows",
			"artifacts":        "200, 1 rows",
			"artifact_summary": "200, 0 rows",
		}))
	})

	It("refuses a viewer's writes to them", func() {
		Expect(map[string]string{
			// Deleting the view deletes its panels, so they're rewritten first.
			"PATCH view_panels":       serve(http.MethodPatch, "/db/view_panels", `UPDATE view_panels SET results = '[{"name":"rewritten"}]' WHERE view_id = ?`, view.ID),
			"DELETE views_summary":    serve(http.MethodDelete, "/db/views_summary", "DELETE FROM views_summary WHERE id = ?", view.ID),
			"DELETE artifacts":        serve(http.MethodDelete, "/db/artifacts", "DELETE FROM artifacts WHERE id = ?", artifact.ID),
			"POST artifacts":          serve(http.MethodPost, "/db/artifacts", "SELECT 1 WHERE false"),
			"DELETE artifact_summary": serve(http.MethodDelete, "/db/artifact_summary", "SELECT 1 WHERE false"),
		}).To(Equal(map[string]string{
			"DELETE views_summary":    "405, 0 rows",
			"PATCH view_panels":       "405, 0 rows",
			"DELETE artifacts":        "405, 0 rows",
			"POST artifacts":          "405, 0 rows",
			"DELETE artifact_summary": "405, 0 rows",
		}))

		var count int64
		Expect(DefaultContext.DB().Model(&models.View{}).Where("id = ?", view.ID).Count(&count).Error).To(Succeed())
		Expect(count).To(Equal(int64(1)))
		Expect(DefaultContext.DB().Model(&models.Artifact{}).Where("id = ?", artifact.ID).Count(&count).Error).To(Succeed())
		Expect(count).To(Equal(int64(1)))
		var results string
		Expect(DefaultContext.DB().Raw("SELECT results::text FROM view_panels WHERE view_id = ?", view.ID).Scan(&results).Error).To(Succeed())
		Expect(results).To(ContainSubstring("original"))
	})

	It("refuses writes to them even to a subject granted writing their objects", func() {
		for _, object := range []string{policy.ObjectViews, policy.ObjectArtifact} {
			for _, action := range []string{policy.ActionCreate, policy.ActionUpdate, policy.ActionDelete} {
				_, err := rbac.Enforcer().AddPolicy(writer.ID.String(), object, action, "allow", "", "legacy")
				Expect(err).ToNot(HaveOccurred())
			}
		}
		DeferCleanup(func() {
			_, err := rbac.Enforcer().DeletePermissionsForUser(writer.ID.String())
			Expect(err).ToNot(HaveOccurred())
		})
		for _, table := range []string{"views_summary", "view_panels", "artifacts", "artifact_summary"} {
			for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
				Expect(serveAsPerson(writer, method, "/db/"+table, "SELECT 1 WHERE false")).To(Equal("405, 0 rows"), method+" "+table)
			}
		}
	})
})
