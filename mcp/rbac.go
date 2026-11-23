package mcp

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/samber/lo"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/db"
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
	toolCreateNotificationSilence = "create_notification_silence"
	toolListNotificationSilences  = "list_notification_silences"
	toolDeleteNotificationSilence = "delete_notification_silence"
	toolCheckAccess               = "check_access"
)

// rbacKinds are the kinds the RBAC tools take, each the path of its /rbac routes and the object they check.
var rbacKinds = []string{adapter.ObjectScopes, adapter.ObjectRoles, adapter.ObjectRoleBindings}

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

func listNotificationSilencesHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if !icrbac.CheckContext(ctx, adapter.ObjectNotificationSilences, policy.ActionRead) {
		return mcp.NewToolResultError(fmt.Sprintf("forbidden: %s does not have %s permission on %s", ctx.Subject(), policy.ActionRead, adapter.ObjectNotificationSilences)), nil
	}

	query := ctx.DB().Where("deleted_at IS NULL").Order("created_at DESC")
	if !req.GetBool("include_expired", false) {
		query = query.Where("until IS NULL OR until > NOW()")
	}
	if search := req.GetString("search", ""); search != "" {
		query = query.Where("name ILIKE ? OR description ILIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var silences []models.NotificationSilence
	if err := query.Find(&silences).Error; err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return structToMCPResponse(req, lo.Map(silences, func(s models.NotificationSilence, _ int) silenceResponse {
		resource := lo.Ternary(s.ConfigID != nil, "config "+lo.FromPtr(s.ConfigID), "")
		for kind, id := range map[string]*string{"component": s.ComponentID, "check": s.CheckID, "canary": s.CanaryID} {
			if id != nil {
				resource = kind + " " + *id
			}
		}
		return silenceResponse{
			ID: s.ID, Name: s.Name, Namespace: s.Namespace, Description: lo.FromPtr(s.Description), Source: s.Source,
			From: s.From, Until: s.Until, Resource: resource, Recursive: s.Recursive, Filter: string(s.Filter),
			Selectors: string(s.Selectors), Error: lo.FromPtr(s.Error),
		}
	})), nil
}

type silenceResponse struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Namespace   string     `json:"namespace,omitempty"`
	Description string     `json:"description,omitempty"`
	Source      string     `json:"source"`
	From        *time.Time `json:"from,omitempty"`
	Until       *time.Time `json:"until,omitempty"`
	Resource    string     `json:"resource,omitempty"`
	Recursive   bool       `json:"recursive,omitempty"`
	Filter      string     `json:"filter,omitempty"`
	Selectors   string     `json:"selectors,omitempty"`
	Error       string     `json:"error,omitempty"`
}

func deleteNotificationSilenceHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if !icrbac.CheckContext(ctx, adapter.ObjectNotificationSilences, policy.ActionDelete) {
		return mcp.NewToolResultError(fmt.Sprintf("forbidden: %s does not have %s permission on %s", ctx.Subject(), policy.ActionDelete, adapter.ObjectNotificationSilences)), nil
	}

	id, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if _, err := uuid.Parse(id); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid silence id %q", id)), nil
	}

	var silence models.NotificationSilence
	if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", id).First(&silence).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("notification silence %s not found", id)), nil
		}
		return mcp.NewToolResultError(err.Error()), nil
	}
	if silence.Source == models.SourceCRD {
		return mcp.NewToolResultError(fmt.Sprintf("notification silence %s/%s is managed through a Kubernetes NotificationSilence and can only be deleted there", silence.Namespace, silence.Name)), nil
	}

	if err := db.DeleteNotificationSilence(ctx, id); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("notification silence %s deleted", id)), nil
}

func checkAccessHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if !icrbac.CheckContext(ctx, policy.ObjectRBAC, adapter.ActionAccessReview) {
		return mcp.NewToolResultError(fmt.Sprintf("forbidden: %s does not have %s permission on %s", ctx.Subject(), adapter.ActionAccessReview, policy.ObjectRBAC)), nil
	}

	raw, err := json.Marshal(req.GetArguments())
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var review icrbac.SubjectAccessReviewRequest
	if err := json.Unmarshal(raw, &review); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid arguments: %v", err)), nil
	}

	// Subjects are ids; people may also be named by email and teams by name
	for i, subject := range review.Subjects {
		if _, err := uuid.Parse(subject); err == nil || subject == "*" || strings.Contains(subject, ":") {
			continue
		}
		var id uuid.UUID
		if strings.Contains(subject, "@") {
			id, err = findPersonID(ctx, subject)
		} else {
			id, err = findTeamID(ctx, subject)
		}
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		review.Subjects[i] = id.String()
	}

	response, err := icrbac.RunSubjectAccessReview(ctx, review)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	// JSON, as the explained rules are nested too deep for a table
	out, err := json.Marshal(response)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(out)), nil
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
			"A RoleBinding is what grants access: to grant a permission, reuse a Scope and Role found with list_rbac_objects, create the ones missing, then create the RoleBinding. "+
			"Objects can be created in any order."),
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

	s.AddTool(mcp.NewTool(toolListNotificationSilences,
		mcp.WithDescription("List notification silences, newest first. Expired silences are left out unless include_expired is set."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("search", mcp.Description("Only list silences whose name or description contains this, case-insensitive")),
		mcp.WithBoolean("include_expired", mcp.Description("Also list silences whose until has passed")),
	), listNotificationSilencesHandler)

	s.AddTool(mcp.NewTool(toolDeleteNotificationSilence,
		mcp.WithDescription("Delete a notification silence, so the notifications it stopped are sent again. Silences managed through Kubernetes can't be deleted here."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithString("id", mcp.Required(), mcp.Description("UUID of the silence, from list_notification_silences")),
	), deleteNotificationSilenceHandler)

	s.AddTool(mcp.NewTool(toolCheckAccess,
		mcp.WithDescription("Check whether subjects may perform an action on a resource, and with explain, which rules decide it. "+
			"Use it to answer why someone can or can't do something."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("action", mcp.Required(), mcp.Description("The action, e.g. read, update, playbook:run, playbook:approve, connection:use")),
		mcp.WithObject("resource", mcp.Required(), mcp.Description("The resource, by UUID: one of {playbook, config, component, check, view, connection}, "+
			"or {global: <RBAC object>} to check the action on a whole type, e.g. {global: \"catalog\"}. Use {} for an action that takes no resource.")),
		mcp.WithArray("subjects", mcp.Required(), mcp.WithStringItems(),
			mcp.Description("People by email or UUID, teams by name or UUID, or [\"*\"] for every subject the policy grants something to")),
		mcp.WithBoolean("explain", mcp.Description("Include the rules that decide each subject's answer")),
	), checkAccessHandler)
}
