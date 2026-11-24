package mcp

import (
	gocontext "context"
	"maps"

	"github.com/flanksource/duty/rbac/policy"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/samber/lo"

	icrbac "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

type toolPermission struct {
	object, action string
}

// toolPermissions are the permissions on a whole object a tool's handler refuses a caller without.
var toolPermissions = map[string]toolPermission{
	toolListConnections:             {policy.ObjectConnection, policy.ActionRead},
	toolRunHealthCheck:              {policy.ObjectCanary, policy.ActionCreate},
	toolListJobs:                    {policy.ObjectJobs, policy.ActionJobRead},
	toolListJobRuns:                 {policy.ObjectJobs, policy.ActionJobRead},
	toolGetJobRun:                   {policy.ObjectJobs, policy.ActionJobRead},
	toolGetNotificationDetail:       {policy.ObjectNotification, policy.ActionRead},
	toolGetNotificationsForResource: {policy.ObjectNotification, policy.ActionRead},
	toolListNotificationSends:       {policy.ObjectNotification, policy.ActionRead},
	toolResolveExternalUser:         {policy.ObjectCatalog, policy.ActionRead},
	toolResolveExternalGroup:        {policy.ObjectCatalog, policy.ActionRead},
	toolCreateNotificationSilence:   {adapter.ObjectNotificationSilences, policy.ActionCreate},
	toolListTeams:                   {adapter.ObjectTeams, policy.ActionRead},
	toolListTeamMembers:             {adapter.ObjectTeams, policy.ActionRead},
	toolListPersonTeams:             {adapter.ObjectTeams, policy.ActionRead},
	toolAddTeamMembers:              {adapter.ObjectTeams, policy.ActionUpdate},
	toolRemoveTeamMember:            {adapter.ObjectTeams, policy.ActionUpdate},
	toolCreateTeam:                  {adapter.ObjectTeams, policy.ActionCreate},
	toolUpdateTeam:                  {adapter.ObjectTeams, policy.ActionUpdate},
	toolDeleteTeam:                  {adapter.ObjectTeams, policy.ActionDelete},
	toolGetPerson:                   {policy.ObjectPeople, policy.ActionRead},
	toolListNotificationSilences:    {adapter.ObjectNotificationSilences, policy.ActionRead},
	toolDeleteNotificationSilence:   {adapter.ObjectNotificationSilences, policy.ActionDelete},
	toolCheckAccess:                 {policy.ObjectRBAC, adapter.ActionAccessReview},
}

// resourceToolPermissions are the actions a tool's handler checks on the resource it's called with. The tool is
// offered to a caller who holds the action on any resource, as its handler can't refuse them every call.
var resourceToolPermissions = map[string]toolPermission{
	toolApprovePlaybookRun: {policy.ObjectPlaybooks, policy.ActionPlaybookApprove},
	toolCancelPlaybookRun:  {policy.ObjectPlaybooks, policy.ActionPlaybookCancel},
	toolTestConnection:     {policy.ObjectConnection, adapter.ActionConnectionUse},
}

// rbacToolActions are the actions the RBAC tools require on the kind they're called with, which is the object checked.
var rbacToolActions = map[string]string{
	toolListRBACObjects:  policy.ActionRead,
	toolGetRBACObject:    policy.ActionRead,
	toolCreateRBACObject: policy.ActionCreate,
	toolUpdateRBACObject: policy.ActionUpdate,
	toolDeleteRBACObject: policy.ActionDelete,
}

// filterToolsByPermission hides the tools whose handler would refuse the caller, and limits the kinds an RBAC tool
// offers to those the caller may use it on. mcp-go applies it to tools/list and tools/call, so a hidden tool can't be
// called, and it's checked with the caller's current permissions on every request.
func filterToolsByPermission(goctx gocontext.Context, tools []mcp.Tool) []mcp.Tool {
	ctx, err := getDutyCtx(goctx)
	allowed := func(object, action string) bool {
		return err == nil && icrbac.CheckContext(ctx, object, action)
	}

	return lo.FilterMap(tools, func(tool mcp.Tool, _ int) (mcp.Tool, bool) {
		if p, ok := toolPermissions[tool.Name]; ok {
			return tool, allowed(p.object, p.action)
		}
		if p, ok := resourceToolPermissions[tool.Name]; ok {
			return tool, err == nil && icrbac.HoldsAction(ctx, p.object, p.action)
		}
		if action, ok := rbacToolActions[tool.Name]; ok {
			kinds := lo.Filter(rbacKinds, func(kind string, _ int) bool { return allowed(kind, action) })
			return withKinds(tool, kinds), len(kinds) > 0
		}
		return tool, true
	})
}

// withKinds returns a copy of the RBAC tool that offers only the kinds.
func withKinds(tool mcp.Tool, kinds []string) mcp.Tool {
	kind, ok := tool.InputSchema.Properties["kind"].(map[string]any)
	if !ok {
		return tool
	}
	kind = maps.Clone(kind)
	kind["enum"] = kinds
	tool.InputSchema.Properties = maps.Clone(tool.InputSchema.Properties)
	tool.InputSchema.Properties["kind"] = kind
	return tool
}
