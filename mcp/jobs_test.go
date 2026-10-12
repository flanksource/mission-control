package mcp

import (
	"bytes"
	gocontext "context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/auth/signing"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = ginkgo.Describe("Job tools", ginkgo.Ordered, func() {
	var creator, scopedToken models.Person

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
		_, _, err := signing.Initialize("/tmp/dummy")
		Expect(err).NotTo(HaveOccurred())

		creator = models.Person{ID: uuid.New(), Name: "Job Tools Creator", Email: "job-tools-creator@test.com"}
		Expect(DefaultContext.DB().Create(&creator).Error).To(Succeed())
		_, err = dutyRBAC.Enforcer().AddPermissionsForUser(creator.ID.String(),
			[]string{policy.ObjectCatalog, policy.ActionRead, "allow", "", "na"},
			[]string{adapter.ObjectJobs, adapter.ActionJobRead, "allow", "", "na"},
		)
		Expect(err).NotTo(HaveOccurred())

		body, _ := json.Marshal(auth.CreateTokenRequest{
			Name:  "job-tools-catalog-only",
			Scope: []policy.Permission{{Object: policy.ObjectCatalog, Action: policy.ActionRead}},
		})
		req := httptest.NewRequest(http.MethodPost, "/auth/create_token", bytes.NewBuffer(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req, rec)
		c.SetRequest(req.WithContext(DefaultContext.WithUser(&creator)))
		Expect(auth.CreateToken(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())

		var token models.AccessToken
		Expect(DefaultContext.DB().Where("name = ?", "job-tools-catalog-only").First(&token).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("id = ?", token.PersonID).First(&scopedToken).Error).To(Succeed())
	})

	ginkgo.AfterAll(func() {
		_, err := dutyRBAC.Enforcer().DeletePermissionsForUser(creator.ID.String())
		Expect(err).NotTo(HaveOccurred())
		_, err = dutyRBAC.Enforcer().DeletePermissionsForUser(scopedToken.ID.String())
		Expect(err).NotTo(HaveOccurred())
		_, err = dutyRBAC.Enforcer().DeleteRolesForUser(scopedToken.ID.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(DefaultContext.DB().Where("person_id = ?", scopedToken.ID).Delete(&models.AccessToken{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(&scopedToken).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(&creator).Error).To(Succeed())
	})

	for _, tool := range tools {
		ginkgo.It(tool.name+" refuses a token whose scope doesn't list job:read, though its creator has it", func() {
			result := call(DefaultContext.WithUser(&scopedToken), tool)
			Expect(result.IsError).To(BeTrue())
			checkResultInMCPResponse(result.Content, []string{"forbidden", adapter.ActionJobRead})
		})

		ginkgo.It(tool.name+" allows the creator, who has job:read", func() {
			result := call(DefaultContext.WithUser(&creator), tool)
			checkResultNotInMCPResponse(result.Content, []string{"forbidden"})
		})
	}
})
