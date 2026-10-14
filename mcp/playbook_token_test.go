package mcp

import (
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/auth/signing"
	"github.com/flanksource/incident-commander/db"
	icrbac "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/rbactest"
)

var _ = ginkgo.Describe("Running a playbook through MCP with an access token", ginkgo.Ordered, func() {
	namespace := "mcp-token-" + uuid.NewString()[:8]
	toolName := "mcp-token-run-" + uuid.NewString()[:8]
	var owner, tokenPerson *models.Person

	run := func(subject *models.Person) *mcp.CallToolResult {
		ginkgo.GinkgoHelper()
		ctx := DefaultContext.WithUser(subject).WithSubject(subject.ID.String())
		result, err := playbookRunHandler(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: toolName, Arguments: map[string]any{}}})
		Expect(err).ToNot(HaveOccurred())
		return result
	}

	ginkgo.BeforeAll(func() {
		Expect(icrbac.Init(DefaultContext)).To(Succeed())
		_, _, err := signing.Initialize("/tmp/dummy")
		Expect(err).ToNot(HaveOccurred())

		owner, err = db.CreatePerson(DefaultContext, "MCP Token Owner", "mcp-token-"+uuid.NewString()[:8]+"@test.com", "")
		Expect(err).ToNot(HaveOccurred())

		// The owner runs playbooks, through MCP too. The token's Role only allows mcp:run.
		Expect(rbactest.BindWholeType(DefaultContext, namespace, "owner", []string{owner.Email}, []string{"playbook"},
			[]string{policy.ActionRead, policy.ActionPlaybookRun, policy.ActionMCPRun})).To(Succeed())
		Expect(rbactest.BindWholeType(DefaultContext, namespace, "mcp-only", []string{"nobody@test.com"}, []string{"playbook"},
			[]string{policy.ActionRead, policy.ActionMCPRun})).To(Succeed())

		result, err := db.CreateAccessTokenForPerson(DefaultContext, owner, "mcp-only", 0, false,
			models.AccessTokenRoles{{Namespace: namespace, Name: "mcp-only"}})
		Expect(err).ToNot(HaveOccurred())
		tokenPerson = result.Person
		_, err = dutyRBAC.Enforcer().AddGroupingPolicy(tokenPerson.ID.String(), owner.ID.String())
		Expect(err).ToNot(HaveOccurred())

		playbookToolNameToID.Store(toolName, dummy.EchoConfig.ID.String())
	})

	ginkgo.AfterAll(func() {
		playbookToolNameToID.Delete(toolName)
		DefaultContext.DB().Where("playbook_id = ?", dummy.EchoConfig.ID).Where("created_by IN ?", []uuid.UUID{owner.ID, tokenPerson.ID}).
			Delete(&models.PlaybookRun{})
		Expect(rbactest.DeleteNamespace(DefaultContext, namespace)).To(Succeed())
	})

	ginkgo.It("starts the run for the owner", func() {
		result := run(owner)
		Expect(result.IsError).To(BeFalse(), "%v", result.Content)
	})

	ginkgo.It("limits the run's own checks by the token's Roles", func() {
		result := run(tokenPerson)
		Expect(result.IsError).To(BeTrue())
		checkResultInMCPResponse(result.Content, []string{"cannot run playbook"})
	})
})
