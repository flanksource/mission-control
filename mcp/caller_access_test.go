package mcp

import (
	gocontext "context"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	icrbac "github.com/flanksource/incident-commander/rbac"
)

// Check history, access logs and external identities are in tables without row-level security, so the tools that
// read them must check the caller's access themselves (#3662).
var _ = ginkgo.Describe("Tools over tables without row-level security", ginkgo.Ordered, ginkgo.ContinueOnFailure, func() {
	const namespace = "mcp-caller-access"

	var (
		configA, configB models.ConfigItem
		canaryA, canaryB models.Canary
		checkA, checkB   models.Check
		reader           *models.Person
	)

	ginkgo.BeforeAll(func() {
		Expect(icrbac.Init(DefaultContext)).To(Succeed())

		configA = models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("mcp-caller-access-a"), Type: lo.ToPtr("Test::Access"), ConfigClass: "Test", Tags: map[string]string{"namespace": "tenant-a"}}
		configB = models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("mcp-caller-access-b"), Type: lo.ToPtr("Test::Access"), ConfigClass: "Test", Tags: map[string]string{"namespace": "tenant-b"}}
		canaryA = models.Canary{ID: uuid.New(), Name: "mcp-caller-access-canary-a", Namespace: namespace, Spec: []byte("{}")}
		canaryB = models.Canary{ID: uuid.New(), Name: "mcp-caller-access-canary-b", Namespace: namespace, Spec: []byte("{}")}
		checkA = models.Check{ID: uuid.New(), CanaryID: canaryA.ID, Name: "mcp-caller-access-check-a", Type: "http", Status: "healthy"}
		checkB = models.Check{ID: uuid.New(), CanaryID: canaryB.ID, Name: "mcp-caller-access-check-b", Type: "http", Status: "healthy"}

		// Scopes read stored membership, so the resources they select must exist before they're built
		for _, row := range []any{&configA, &configB, &canaryA, &canaryB, &checkA, &checkB} {
			Expect(DefaultContext.DB().Create(row).Error).To(Succeed())
		}
		now := time.Now()
		for _, check := range []models.Check{checkA, checkB} {
			Expect(DefaultContext.DB().Create(&models.CheckStatus{CheckID: check.ID, Status: true, CreatedAt: now, Time: now.Format(time.DateTime)}).Error).To(Succeed())
		}
		for _, config := range []models.ConfigItem{configA, configB} {
			Expect(DefaultContext.DB().Create(&models.ConfigAccessLog{ConfigID: config.ID, ExternalUserID: dummy.JohnDoeExternalUser.ID, ScraperID: dummy.KubeScrapeConfig.ID, CreatedAt: now}).Error).To(Succeed())
		}

		reader = setup.CreateUserWithRole(DefaultContext, "MCP Caller Access Reader", "mcp-caller-access-reader@test.com")

		uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }
		meta := metav1.ObjectMeta{Name: "tenant-a", Namespace: namespace}
		meta.UID = uid()
		Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{ObjectMeta: meta, Spec: v1.ScopeSpec{Targets: []v1.ScopeTarget{
			{Config: &v1.ScopeConfigSelector{TagSelector: "namespace=tenant-a"}},
			{Canary: &v1.ScopeCanarySelector{ScopeResourceRef: v1.ScopeResourceRef{Name: canaryA.Name}}},
		}}})).To(Succeed())
		meta.UID = uid()
		Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{ObjectMeta: meta, Spec: v1.RoleSpec{Rules: []v1.RoleRule{
			{Name: "read", Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: meta.Name}},
		}}})).To(Succeed())
		meta.UID = uid()
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{ObjectMeta: meta, Spec: v1.RoleBindingSpec{Role: meta.Name, Subjects: v1.RoleBindingSubjects{
			PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{reader.Email}},
		}}})).To(Succeed())
		Expect(dutyRBAC.ReloadPolicy()).To(Succeed())
	})

	ginkgo.AfterAll(func() {
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{}).Error).To(Succeed())
		Expect(dutyRBAC.ReloadPolicy()).To(Succeed())

		Expect(DefaultContext.DB().Where("check_id IN ?", []uuid.UUID{checkA.ID, checkB.ID}).Delete(&models.CheckStatus{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("config_id IN ?", []uuid.UUID{configA.ID, configB.ID}).Delete(&models.ConfigAccessLog{}).Error).To(Succeed())
		for _, row := range []any{&checkA, &checkB, &canaryA, &canaryB, &configA, &configB, reader} {
			Expect(DefaultContext.DB().Delete(row).Error).To(Succeed())
		}
	})

	call := func(person *models.Person, handler func(gocontext.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) (string, bool) {
		ginkgo.GinkgoHelper()
		ctx := DefaultContext.WithUser(person).WithSubject(person.ID.String())
		result, err := handler(ctx, mcp.CallToolRequest{Header: jsonHeader, Params: mcp.CallToolParams{Arguments: args}})
		Expect(err).NotTo(HaveOccurred())
		var text []string
		for _, c := range result.Content {
			if t, ok := c.(mcp.TextContent); ok {
				text = append(text, t.Text)
			}
		}
		return strings.Join(text, "\n"), result.IsError
	}

	ginkgo.Describe("get_check_status", func() {
		ginkgo.It("returns the history of a check the caller can read", func() {
			text, isError := call(reader, checkStatusHandler, map[string]any{"id": checkA.ID.String()})
			Expect(isError).To(BeFalse(), text)
			Expect(text).To(ContainSubstring(checkA.ID.String()))
		})

		ginkgo.It("answers a check the caller can't read like an unknown id", func() {
			text, isError := call(reader, checkStatusHandler, map[string]any{"id": checkB.ID.String()})
			Expect(isError).To(BeTrue(), text)
			Expect(text).To(Equal(fmt.Sprintf("check[%s] not found", checkB.ID)))

			unknown := uuid.NewString()
			text, isError = call(reader, checkStatusHandler, map[string]any{"id": unknown})
			Expect(isError).To(BeTrue(), text)
			Expect(text).To(Equal(fmt.Sprintf("check[%s] not found", unknown)))
		})
	})

	ginkgo.Describe("search_catalog_access_log", func() {
		ginkgo.It("returns the access logs of a config the caller can read", func() {
			text, isError := call(reader, searchCatalogAccessLogHandler, map[string]any{"config_id": configA.ID.String()})
			Expect(isError).To(BeFalse(), text)
			Expect(text).To(ContainSubstring(configA.ID.String()))
		})

		ginkgo.It("answers a config the caller can't read like an unknown id", func() {
			text, isError := call(reader, searchCatalogAccessLogHandler, map[string]any{"config_id": configB.ID.String()})
			Expect(isError).To(BeTrue(), text)
			Expect(text).To(Equal(fmt.Sprintf("config item[%s] not found", configB.ID)))

			unknown := uuid.NewString()
			text, isError = call(reader, searchCatalogAccessLogHandler, map[string]any{"config_id": unknown})
			Expect(isError).To(BeTrue(), text)
			Expect(text).To(Equal(fmt.Sprintf("config item[%s] not found", unknown)))
		})
	})

	ginkgo.Describe("resolve_external_user and resolve_external_group", func() {
		ginkgo.It("resolve for a caller who can read the whole catalog", func() {
			text, isError := call(&dummy.JohnDoe, resolveExternalUserHandler, map[string]any{"query": dummy.JohnDoeExternalUser.Name})
			Expect(isError).To(BeFalse(), text)
			Expect(text).To(ContainSubstring(dummy.JohnDoeExternalUser.ID.String()))

			text, isError = call(&dummy.JohnDoe, resolveExternalGroupHandler, map[string]any{"query": dummy.MissionControlAdminsGroup.Name})
			Expect(isError).To(BeFalse(), text)
			Expect(text).To(ContainSubstring(dummy.MissionControlAdminsGroup.ID.String()))
		})

		ginkgo.It("resolve_external_user is forbidden to a caller who can read only part of the catalog", func() {
			text, isError := call(reader, resolveExternalUserHandler, map[string]any{"query": dummy.JohnDoeExternalUser.Name})
			Expect(isError).To(BeTrue(), text)
			Expect(text).To(Equal(fmt.Sprintf("forbidden: %s does not have %s permission on %s", reader.ID, policy.ActionRead, policy.ObjectCatalog)))
		})

		ginkgo.It("resolve_external_group is forbidden to a caller who can read only part of the catalog", func() {
			text, isError := call(reader, resolveExternalGroupHandler, map[string]any{"query": dummy.MissionControlAdminsGroup.Name})
			Expect(isError).To(BeTrue(), text)
			Expect(text).To(Equal(fmt.Sprintf("forbidden: %s does not have %s permission on %s", reader.ID, policy.ActionRead, policy.ObjectCatalog)))
		})
	})
})
