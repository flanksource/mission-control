package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/notification"
	icrbac "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

const (
	toolListRBACObjects           = "list_rbac_objects"
	toolGetRBACObject             = "get_rbac_object"
	toolCreateRBACObject          = "create_rbac_object"
	toolUpdateRBACObject          = "update_rbac_object"
	toolDeleteRBACObject          = "delete_rbac_object"
	toolGrantAccess               = "grant_access"
	toolCreateNotificationSilence = "create_notification_silence"
)

var rbacKinds = []string{"scopes", "roles", "role-bindings"}

func listRBACObjectsHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	kind, err := req.RequireString("kind")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	list, err := icrbac.ListManagedObjects(ctx, kind, req.GetString("namespace", ""))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, list), nil
}

func getRBACObjectHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	kind, namespace, name, err := requireRBACAddress(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	object, err := icrbac.GetManagedObject(ctx, kind, namespace, name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, object), nil
}

func createRBACObjectHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	kind, err := req.RequireString("kind")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	object, err := requireObjectJSON(req, "object")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	created, err := icrbac.CreateManagedObject(ctx, kind, object)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, created), nil
}

func updateRBACObjectHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	kind, namespace, name, err := requireRBACAddress(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	object, err := requireObjectJSON(req, "object")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	updated, err := icrbac.UpdateManagedObject(ctx, kind, namespace, name, object)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, updated), nil
}

func deleteRBACObjectHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	kind, namespace, name, err := requireRBACAddress(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := icrbac.DeleteManagedObject(ctx, kind, namespace, name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("%s %s/%s deleted", kind, namespace, name)), nil
}

// grantAccessRequest is the input of grant_access.
type grantAccessRequest struct {
	Name            string                 `json:"name"`
	Namespace       string                 `json:"namespace"`
	Description     string                 `json:"description"`
	Action          string                 `json:"action"`
	ResourceScope   string                 `json:"resource_scope"`
	ResourceTargets []v1.ScopeTarget       `json:"resource_targets"`
	TargetScope     string                 `json:"target_scope"`
	TargetTargets   []v1.ScopeTarget       `json:"target_targets"`
	Subjects        v1.RoleBindingSubjects `json:"subjects"`
}

// objects returns the Scopes, Role and RoleBinding that grant the one rule asked for, in the order they're created.
func (r grantAccessRequest) objects() ([]icrbac.ManagedObject, error) {
	if r.Name == "" || r.Namespace == "" || r.Action == "" {
		return nil, fmt.Errorf("name, namespace and action are required")
	}
	if r.ResourceScope != "" && len(r.ResourceTargets) > 0 {
		return nil, fmt.Errorf("set resource_scope or resource_targets, not both")
	}
	if r.TargetScope != "" && len(r.TargetTargets) > 0 {
		return nil, fmt.Errorf("set target_scope or target_targets, not both")
	}

	metadata := func(name string) map[string]any {
		return map[string]any{"name": name, "namespace": r.Namespace}
	}

	var objects []icrbac.ManagedObject
	add := func(kind string, object map[string]any) error {
		raw, err := json.Marshal(object)
		if err != nil {
			return err
		}
		objects = append(objects, icrbac.ManagedObject{Kind: kind, Object: raw})
		return nil
	}

	// scopeRef returns the reference to an existing Scope, or creates a Scope named name from targets
	scopeRef := func(existing string, targets []v1.ScopeTarget, name string) (*v1.ScopeReference, error) {
		if existing != "" {
			return &v1.ScopeReference{ScopeRef: existing}, nil
		} else if len(targets) == 0 {
			return nil, nil
		}
		err := add("scopes", map[string]any{
			"metadata": metadata(name),
			"spec":     v1.ScopeSpec{Description: r.Description, Targets: targets},
		})
		return &v1.ScopeReference{ScopeRef: name}, err
	}

	resource, err := scopeRef(r.ResourceScope, r.ResourceTargets, r.Name+"-resource")
	if err != nil {
		return nil, err
	}
	target, err := scopeRef(r.TargetScope, r.TargetTargets, r.Name+"-target")
	if err != nil {
		return nil, err
	}

	rule := map[string]any{"name": r.Name, "action": r.Action}
	if resource != nil {
		rule["resource"] = resource
	}
	if target != nil {
		rule["target"] = target
	}
	if err := add("roles", map[string]any{
		"metadata": metadata(r.Name),
		"spec":     map[string]any{"description": r.Description, "rules": []any{rule}},
	}); err != nil {
		return nil, err
	}

	if err := add("role-bindings", map[string]any{
		"metadata": metadata(r.Name),
		"spec":     v1.RoleBindingSpec{Description: r.Description, Role: r.Name, Subjects: r.Subjects},
	}); err != nil {
		return nil, err
	}
	return objects, nil
}

func grantAccessHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var grant grantAccessRequest
	if err := req.BindArguments(&grant); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid arguments: %v", err)), nil
	}
	objects, err := grant.objects()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	created, err := icrbac.CreateManagedObjects(ctx, objects)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, created...), nil
}

func createNotificationSilenceHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if !icrbac.CheckContext(ctx, adapter.ObjectNotificationSilences, policy.ActionCreate) {
		return mcp.NewToolResultError(fmt.Sprintf("forbidden: %s does not have %s permission on %s", ctx.Subject(), policy.ActionCreate, adapter.ObjectNotificationSilences)), nil
	}

	raw, err := json.Marshal(req.GetArguments())
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var silence notification.SilenceSaveRequest
	if err := json.Unmarshal(raw, &silence); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	silence.Source = models.SourceUI
	saved, err := notification.SaveNotificationSilence(ctx, silence)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, saved), nil
}

func requireRBACAddress(req mcp.CallToolRequest) (kind, namespace, name string, err error) {
	if kind, err = req.RequireString("kind"); err != nil {
		return
	}
	if namespace, err = req.RequireString("namespace"); err != nil {
		return
	}
	name, err = req.RequireString("name")
	return
}

// requireObjectJSON returns the argument, an object, as JSON.
func requireObjectJSON(req mcp.CallToolRequest, key string) ([]byte, error) {
	object, ok := req.GetArguments()[key].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	return json.Marshal(object)
}

func registerRBAC(s *server.MCPServer) {
	kind := func(required bool) mcp.ToolOption {
		opts := []mcp.PropertyOption{
			mcp.Enum(rbacKinds...),
			mcp.Description("Kind of object: scopes, roles or role-bindings"),
		}
		if required {
			opts = append(opts, mcp.Required())
		}
		return mcp.WithString("kind", opts...)
	}
	namespace := mcp.WithString("namespace", mcp.Required(), mcp.Description("Namespace of the object"))
	name := mcp.WithString("name", mcp.Required(), mcp.Description("Name of the object"))
	object := mcp.WithObject("object", mcp.Required(),
		mcp.Description("The object as the /rbac API and the CRD take it: {metadata: {name, namespace}, spec: {...}}. "+
			"Scope spec: {description, targets: [{config|component|check|canary|playbook|connection|scraper|view: {id, name, namespace, agent, types, tagSelector, labelSelector}} or {<type>: {name: \"*\"}}]}. "+
			"Role spec: {description, rules: [{name, action, resource: {scopeRef}, target: {scopeRef}}]}. "+
			"RoleBinding spec: {description, role, subjects: {people, teams, serviceAccounts, playbooks, notifications, ...}, constraint: {resource: {scopeRef}, target: {scopeRef}}, deny}."),
	)

	s.AddTool(mcp.NewTool(toolListRBACObjects,
		mcp.WithDescription("List Mission Control Scopes, Roles or RoleBindings, with whether each is in effect (error, error_reason)."),
		mcp.WithReadOnlyHintAnnotation(true),
		kind(true),
		mcp.WithString("namespace", mcp.Description("Only list objects in this namespace. Lists every namespace when omitted.")),
	), listRBACObjectsHandler)

	s.AddTool(mcp.NewTool(toolGetRBACObject,
		mcp.WithDescription("Get one Mission Control Scope, Role or RoleBinding by namespace and name, with whether it's in effect and, for Roles and RoleBindings, which rules don't apply through a binding."),
		mcp.WithReadOnlyHintAnnotation(true),
		kind(true), namespace, name,
	), getRBACObjectHandler)

	s.AddTool(mcp.NewTool(toolCreateRBACObject,
		mcp.WithDescription("Create a Mission Control Scope (a set of resources), Role (rules: an action on a Scope) or RoleBinding (grants a Role to subjects). "+
			"An object that's invalid on its own is rejected. One whose references are missing or don't fit is stored, and the result says why it isn't in effect. "+
			"To grant a single permission in one call, use grant_access."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		kind(true), object,
	), createRBACObjectHandler)

	s.AddTool(mcp.NewTool(toolUpdateRBACObject,
		mcp.WithDescription("Replace a Mission Control Scope, Role or RoleBinding created through the UI, API or MCP. Objects managed through Kubernetes can't be changed here."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		kind(true), namespace, name, object,
	), updateRBACObjectHandler)

	s.AddTool(mcp.NewTool(toolDeleteRBACObject,
		mcp.WithDescription("Delete a Mission Control Scope, Role or RoleBinding created through the UI, API or MCP. Deleting a RoleBinding revokes what it grants."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		kind(true), namespace, name,
	), deleteRBACObjectHandler)

	scopeTargets := mcp.Items(map[string]any{
		"type":        "object",
		"description": "One Scope target, selecting resources of exactly one type, e.g. {\"playbook\": {\"name\": \"restart-pod\"}} or {\"config\": {\"tagSelector\": \"env=staging\"}}",
	})
	s.AddTool(mcp.NewTool(toolGrantAccess,
		mcp.WithDescription("Grant one permission in a single call, e.g. let alice@example.com run the restart-pod playbook on staging configs. "+
			"Creates, all or nothing: Scopes <name>-resource and <name>-target from the targets given, a Role <name> with one rule, and a RoleBinding <name> of it to the subjects. "+
			"It grants exactly the action asked for: running a playbook on a config also needs read on that config, granted separately. "+
			"Revoke with delete_rbac_object on the RoleBinding."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithString("name", mcp.Required(), mcp.Description("Name of the Role and RoleBinding, and prefix of the Scopes created")),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("Namespace of every object created")),
		mcp.WithString("action", mcp.Required(), mcp.Description("The rule's action, e.g. read, playbook:run, playbook:approve, connection:use, mcp:run, invoke:<plugin>:<operation>, or an action without a resource such as mcp:use")),
		mcp.WithString("resource_scope", mcp.Description("An existing Scope for the rule's resource, as name or namespace/name. Mutually exclusive with resource_targets.")),
		mcp.WithArray("resource_targets", scopeTargets, mcp.Description("Targets of a new Scope for the rule's resource. Omit both resource fields for actions without a resource.")),
		mcp.WithString("target_scope", mcp.Description("An existing Scope for the rule's target, e.g. the configs a playbook runs on. Mutually exclusive with target_targets.")),
		mcp.WithArray("target_targets", scopeTargets, mcp.Description("Targets of a new Scope for the rule's target")),
		mcp.WithObject("subjects", mcp.Required(), mcp.Description("Who gets the permission: {people: [emails], teams: [names], serviceAccounts: [names], playbooks|notifications|...: [{namespace, name}]}")),
		mcp.WithString("description", mcp.Description("Description set on every object created")),
	), grantAccessHandler)

	s.AddTool(mcp.NewTool(toolCreateNotificationSilence,
		mcp.WithDescription("Create a notification silence, which stops notifications for matching resources between from and until. At least one of config_id, component_id, check_id, canary_id, filter or selectors is required."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithString("name", mcp.Required(), mcp.Description("Name of the silence")),
		mcp.WithString("description", mcp.Description("Why the notifications are silenced")),
		mcp.WithString("from", mcp.Description("Start, as datemath, e.g. now. Starts immediately when omitted.")),
		mcp.WithString("until", mcp.Description("End, as datemath, e.g. now+4h")),
		mcp.WithBoolean("recursive", mcp.Description("Also silence the resource's children")),
		mcp.WithString("config_id", mcp.Description("UUID of a config item to silence")),
		mcp.WithString("component_id", mcp.Description("UUID of a component to silence")),
		mcp.WithString("check_id", mcp.Description("UUID of a health check to silence")),
		mcp.WithString("canary_id", mcp.Description("UUID of a canary to silence")),
		mcp.WithString("filter", mcp.Description("CEL expression a notification's event must match to be silenced")),
		mcp.WithArray("selectors", mcp.Items(map[string]any{"type": "object"}), mcp.Description("Resource selectors of the resources to silence, e.g. [{\"types\": [\"Kubernetes::Pod\"], \"namespace\": \"staging\"}]")),
	), createNotificationSilenceHandler)
}
