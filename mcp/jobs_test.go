package mcp

import (
	gocontext "context"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Job tools", ginkgo.Ordered, func() {
	var jobReader, catalogReader models.Person

	type jobTool struct {
		name    string
		handler func(gocontext.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args    map[string]any
	}

	tools := []jobTool{
		{toolListJobs, listJobsHandler, nil},
		{toolListJobRuns, listJobRunsHandler, nil},
		{toolGetJobRun, getJobRunHandler, map[string]any{"id": uuid.NewString()}},
	}

	call := func(ctx context.Context, tool jobTool) *mcp.CallToolResult {
		ginkgo.GinkgoHelper()
		result, err := tool.handler(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: tool.name, Arguments: tool.args}})
		Expect(err).NotTo(HaveOccurred())
		return result
	}

	ginkgo.BeforeAll(func() {
		jobReader = models.Person{ID: uuid.New(), Name: "Job Tools Reader", Email: "job-tools-reader@test.com"}
		catalogReader = models.Person{ID: uuid.New(), Name: "Job Tools Catalog Reader", Email: "job-tools-catalog-reader@test.com"}
		Expect(DefaultContext.DB().Create(&jobReader).Error).To(Succeed())
		Expect(DefaultContext.DB().Create(&catalogReader).Error).To(Succeed())

		_, err := dutyRBAC.Enforcer().AddPermissionsForUser(jobReader.ID.String(),
			[]string{policy.ObjectJobs, policy.ActionJobRead, "allow", "", "na"},
		)
		Expect(err).NotTo(HaveOccurred())
		_, err = dutyRBAC.Enforcer().AddPermissionsForUser(catalogReader.ID.String(),
			[]string{policy.ObjectCatalog, policy.ActionRead, "allow", "", "na"},
		)
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.AfterAll(func() {
		for _, p := range []models.Person{jobReader, catalogReader} {
			_, err := dutyRBAC.Enforcer().DeletePermissionsForUser(p.ID.String())
			Expect(err).NotTo(HaveOccurred())
			Expect(DefaultContext.DB().Delete(&p).Error).To(Succeed())
		}
	})

	for _, tool := range tools {
		ginkgo.It(tool.name+" refuses a subject without job:read", func() {
			result := call(DefaultContext.WithUser(&catalogReader), tool)
			Expect(result.IsError).To(BeTrue())
			checkResultInMCPResponse(result.Content, []string{"forbidden", policy.ActionJobRead})
		})

		ginkgo.It(tool.name+" allows a subject with job:read", func() {
			result := call(DefaultContext.WithUser(&jobReader), tool)
			checkResultNotInMCPResponse(result.Content, []string{"forbidden"})
		})
	}
})
