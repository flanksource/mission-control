package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

var _ = ginkgo.Describe("MCP reads follow the caller's access", ginkgo.Ordered, ginkgo.ContinueOnFailure, func() {
	const (
		namespace = "mcp-lookup-access"
		childType = "MCPLookupAccess::Child"
	)

	var (
		parent, configA, configB models.ConfigItem
		canaryA, canaryB         models.Canary
		checkA, checkB           models.Check
		playbookA, playbookB     models.Playbook
		reader                   *models.Person
		readerCtx, adminCtx      context.Context
	)

	ginkgo.BeforeAll(func() {
		newConfig := func(name, typ, tenant string) models.ConfigItem {
			return models.ConfigItem{
				ID: uuid.New(), Name: lo.ToPtr(name), Type: lo.ToPtr(typ), ConfigClass: "MCPLookupAccess",
				Tags:   types.JSONStringMap{"namespace": tenant},
				Config: lo.ToPtr(fmt.Sprintf(`{"name": %q}`, name)),
			}
		}
		parent = newConfig("mcp-lookup-parent", "MCPLookupAccess::Parent", "mcp-lookup-tenant-a")
		configA = newConfig("mcp-lookup-config-a", childType, "mcp-lookup-tenant-a")
		configB = newConfig("mcp-lookup-config-b", childType, "mcp-lookup-tenant-b")
		canaryA = models.Canary{ID: uuid.New(), Name: "mcp-lookup-canary-a", Namespace: namespace, Spec: []byte("{}")}
		canaryB = models.Canary{ID: uuid.New(), Name: "mcp-lookup-canary-b", Namespace: namespace, Spec: []byte("{}")}
		checkA = models.Check{ID: uuid.New(), CanaryID: canaryA.ID, Name: "mcp-lookup-check-a", Type: "http", Status: "healthy"}
		checkB = models.Check{ID: uuid.New(), CanaryID: canaryB.ID, Name: "mcp-lookup-check-b", Type: "http", Status: "healthy"}
		playbookA = models.Playbook{ID: uuid.New(), Name: "mcp-lookup-playbook-a", Namespace: namespace, Source: models.SourceUI, Spec: []byte(`{"actions":[]}`)}
		playbookB = models.Playbook{ID: uuid.New(), Name: "mcp-lookup-playbook-b", Namespace: namespace, Source: models.SourceUI, Spec: []byte(`{"actions":[]}`)}

		// Scopes read stored membership, so the resources they select must exist before they're built
		for _, row := range []any{&parent, &configA, &configB, &canaryA, &canaryB, &checkA, &checkB, &playbookA, &playbookB} {
			Expect(DefaultContext.DB().Create(row).Error).To(Succeed())
		}
		Expect(DefaultContext.DB().Create(&[]models.ConfigRelationship{
			{ConfigID: configA.ID.String(), RelatedID: parent.ID.String(), Relation: "MCPLookupAccess"},
			{ConfigID: configB.ID.String(), RelatedID: parent.ID.String(), Relation: "MCPLookupAccess"},
		}).Error).To(Succeed())

		var err error
		reader, err = db.CreatePerson(DefaultContext, "MCP Lookup Access Reader", "mcp-lookup-access-reader@test.com", "")
		Expect(err).NotTo(HaveOccurred())

		meta := func() metav1.ObjectMeta {
			return metav1.ObjectMeta{Name: namespace, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())}
		}
		Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{ObjectMeta: meta(), Spec: v1.ScopeSpec{Targets: []v1.ScopeTarget{
			{Config: &v1.ScopeConfigSelector{TagSelector: "namespace=mcp-lookup-tenant-a"}},
			{Canary: &v1.ScopeCanarySelector{ScopeResourceRef: v1.ScopeResourceRef{ID: canaryA.ID.String()}}},
			{Playbook: &v1.ScopePlaybookRef{ID: playbookA.ID.String()}},
		}}})).To(Succeed())
		Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{ObjectMeta: meta(), Spec: v1.RoleSpec{Rules: []v1.RoleRule{
			{Name: "read", Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: namespace}},
		}}})).To(Succeed())
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{ObjectMeta: meta(), Spec: v1.RoleBindingSpec{Role: namespace, Subjects: v1.RoleBindingSubjects{
			PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{reader.Email}},
		}}})).To(Succeed())
		Expect(dutyRBAC.ReloadPolicy()).To(Succeed())

		readerCtx = DefaultContext.WithUser(reader).WithSubject(reader.ID.String())
		adminCtx = DefaultContext.WithUser(&dummy.JohnDoe).WithSubject(dummy.JohnDoe.ID.String())
	})

	ginkgo.AfterAll(func() {
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{}).Error).To(Succeed())
		Expect(dutyRBAC.ReloadPolicy()).To(Succeed())
		Expect(DefaultContext.DB().Where("related_id = ?", parent.ID).Delete(&models.ConfigRelationship{}).Error).To(Succeed())
		for _, row := range []any{&playbookA, &playbookB, &checkA, &checkB, &canaryA, &canaryB, &configA, &configB, &parent, reader} {
			Expect(DefaultContext.DB().Delete(row).Error).To(Succeed())
		}
	})

	toolText := func(result *mcp.CallToolResult) string {
		ginkgo.GinkgoHelper()
		Expect(result.Content).To(HaveLen(1))
		text, ok := result.Content[0].(mcp.TextContent)
		Expect(ok).To(BeTrue())
		return text.Text
	}

	callTool := func(ctx context.Context, handler server.ToolHandlerFunc, name string, args map[string]any) *mcp.CallToolResult {
		ginkgo.GinkgoHelper()
		result, err := handler(ctx, mcp.CallToolRequest{Header: jsonHeader, Params: mcp.CallToolParams{Name: name, Arguments: args}})
		Expect(err).NotTo(HaveOccurred())
		return result
	}

	describeConfig := func(ctx context.Context, id string) *mcp.CallToolResult {
		return callTool(ctx, describeConfigHandler, "describe_config", map[string]any{"id": id})
	}

	evalTemplate := func(ctx context.Context, args map[string]any) *mcp.CallToolResult {
		return callTool(ctx, runTemplateHandler, toolRunTemplate, args)
	}

	// rendered is the string a successful eval_template call renders.
	rendered := func(result *mcp.CallToolResult) string {
		ginkgo.GinkgoHelper()
		Expect(result.IsError).To(BeFalse(), toolText(result))
		var out string
		Expect(json.Unmarshal([]byte(toolText(result)), &out)).To(Succeed())
		return out
	}

	readResource := func(ctx context.Context, handler server.ResourceHandlerFunc, uri string) (string, error) {
		contents, err := handler(ctx, mcp.ReadResourceRequest{Params: mcp.ReadResourceParams{URI: uri}})
		if err != nil {
			return "", err
		}
		return fmt.Sprint(contents), nil
	}

	// expectNotFound asserts the caller gets the answer for an id that doesn't exist, with only the id differing.
	expectNotFound := func(invisibleID string, answer func(id string) string) {
		ginkgo.GinkgoHelper()
		unknownID := uuid.NewString()
		got := answer(invisibleID)
		Expect(got).To(ContainSubstring("not found"))
		Expect(strings.ReplaceAll(got, invisibleID, "<id>")).To(Equal(strings.ReplaceAll(answer(unknownID), unknownID, "<id>")))
	}

	ginkgo.Describe("lookups by id", func() {
		ginkgo.It("describe_config answers not found for a config the caller can't read", func() {
			warm := describeConfig(adminCtx, configB.ID.String())
			Expect(warm.IsError).To(BeFalse(), toolText(warm))

			expectNotFound(configB.ID.String(), func(id string) string {
				result := describeConfig(readerCtx, id)
				Expect(result.IsError).To(BeTrue())
				return toolText(result)
			})

			allowed := describeConfig(readerCtx, configA.ID.String())
			Expect(allowed.IsError).To(BeFalse(), toolText(allowed))
			Expect(toolText(allowed)).To(ContainSubstring(configA.ID.String()))
		})

		ginkgo.It("the config_item resource answers not found for a config the caller can't read", func() {
			_, err := readResource(adminCtx, ConfigItemResourceHandler, "config_item://"+configB.ID.String())
			Expect(err).NotTo(HaveOccurred())

			expectNotFound(configB.ID.String(), func(id string) string {
				_, err := readResource(readerCtx, ConfigItemResourceHandler, "config_item://"+id)
				Expect(err).To(HaveOccurred())
				return err.Error()
			})

			got, err := readResource(readerCtx, ConfigItemResourceHandler, "config_item://"+configA.ID.String())
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(ContainSubstring(configA.ID.String()))
		})

		ginkgo.It("eval_template's config_id answers not found for a config the caller can't read", func() {
			warm := evalTemplate(adminCtx, map[string]any{"config_id": configB.ID.String(), "cel_expression": "config.name"})
			Expect(rendered(warm)).To(Equal(*configB.Name))

			expectNotFound(configB.ID.String(), func(id string) string {
				result := evalTemplate(readerCtx, map[string]any{"config_id": id, "cel_expression": "config.name"})
				Expect(result.IsError).To(BeTrue())
				return toolText(result)
			})

			allowed := evalTemplate(readerCtx, map[string]any{"config_id": configA.ID.String(), "cel_expression": "config.name"})
			Expect(rendered(allowed)).To(Equal(*configA.Name))
		})

		ginkgo.It("eval_template's check_id answers not found for a check the caller can't read", func() {
			warm := evalTemplate(adminCtx, map[string]any{"check_id": checkB.ID.String(), "cel_expression": "check.name"})
			Expect(rendered(warm)).To(Equal(checkB.Name))

			expectNotFound(checkB.ID.String(), func(id string) string {
				result := evalTemplate(readerCtx, map[string]any{"check_id": id, "cel_expression": "check.name"})
				Expect(result.IsError).To(BeTrue())
				return toolText(result)
			})

			allowed := evalTemplate(readerCtx, map[string]any{"check_id": checkA.ID.String(), "cel_expression": "check.name"})
			Expect(rendered(allowed)).To(Equal(checkA.Name))
		})

		ginkgo.It("the playbook resource answers not found for a playbook the caller can't read", func() {
			_, err := readResource(adminCtx, playbookResourceHandler, "playbook://"+playbookB.ID.String())
			Expect(err).NotTo(HaveOccurred())

			expectNotFound(playbookB.ID.String(), func(id string) string {
				_, err := readResource(readerCtx, playbookResourceHandler, "playbook://"+id)
				Expect(err).To(HaveOccurred())
				return err.Error()
			})

			got, err := readResource(readerCtx, playbookResourceHandler, "playbook://"+playbookA.ID.String())
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(ContainSubstring(playbookA.ID.String()))
		})
	})

	ginkgo.Describe("eval_template helpers", func() {
		names := func(ctx context.Context, expression string) []string {
			ginkgo.GinkgoHelper()
			got := lo.Compact(strings.Split(rendered(evalTemplate(ctx, map[string]any{"cel_expression": expression})), ","))
			sort.Strings(got)
			return got
		}

		ginkgo.It("catalog.query lists only configs the caller can read", func() {
			expression := fmt.Sprintf(`dyn(catalog.query("type=%s")).map(c, c.name).join(",")`, childType)
			Expect(names(adminCtx, expression)).To(Equal([]string{*configA.Name, *configB.Name}))
			Expect(names(readerCtx, expression)).To(Equal([]string{*configA.Name}))
		})

		ginkgo.It("catalog.traverse returns only related configs the caller can read", func() {
			expression := fmt.Sprintf(`dyn(catalog.traverse("%s", "%s", "incoming")).map(c, c.name).join(",")`, parent.ID, childType)
			Expect(names(adminCtx, expression)).To(Equal([]string{*configA.Name, *configB.Name}))
			Expect(names(readerCtx, expression)).To(Equal([]string{*configA.Name}))

			goTemplate := fmt.Sprintf(`{{ range catalog_traverse "%s" "%s" }}{{ .Name }},{{ end }}`, parent.ID, childType)
			Expect(rendered(evalTemplate(readerCtx, map[string]any{"gotemplate": goTemplate}))).To(Equal(*configA.Name + ","))

			// Internal callers, without row filters, keep duty's traversal
			internal, err := DefaultContext.RunTemplate(gomplate.Template{Expression: expression}, map[string]any{})
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Split(internal, ",")).To(ConsistOf(*configA.Name, *configB.Name))
		})

		ginkgo.It("gitops.source returns an empty source to callers under row filters", func() {
			expression := fmt.Sprintf(`gitops.source("%s").git.file`, dummy.Namespace.ID)
			// Internal callers, without row filters, keep duty's gitops.source
			warm, err := DefaultContext.RunTemplate(gomplate.Template{Expression: expression}, map[string]any{})
			Expect(err).NotTo(HaveOccurred())
			Expect(warm).To(Equal("aws-demo/spec/namespaces/flux/namespace.yaml"))

			Expect(rendered(evalTemplate(readerCtx, map[string]any{"cel_expression": expression}))).To(BeEmpty())
		})
	})
})
