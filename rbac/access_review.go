package rbac

import (
	"cmp"
	"errors"
	"slices"
	"strings"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/membership"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"gorm.io/gorm"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

const MaxSubjectAccessReviewSubjects = 500

// MaxExplainedSubjects bounds the subjects a review explains at once: explaining a subject reads every rule it holds.
const MaxExplainedSubjects = 50

// SubjectAccessReviewResource is what a review checks the action on: a resource, with a target for actions that take
// one (e.g. a playbook and the config to run it on), or a whole RBAC object.
type SubjectAccessReviewResource struct {
	Playbook   string `json:"playbook,omitempty"`
	Config     string `json:"config,omitempty"`
	Component  string `json:"component,omitempty"`
	Check      string `json:"check,omitempty"`
	View       string `json:"view,omitempty"`
	Connection string `json:"connection,omitempty"`

	// Global is an RBAC object, e.g. catalog, to check the action on as a whole.
	Global string `json:"global,omitempty"`
}

// ids returns the resources the review names, by type.
func (r SubjectAccessReviewResource) ids() map[string]string {
	ids := map[string]string{
		policy.ResourcePlaybook:   r.Playbook,
		policy.ResourceConfig:     r.Config,
		policy.ResourceComponent:  r.Component,
		policy.ResourceCheck:      r.Check,
		policy.ResourceView:       r.View,
		policy.ResourceConnection: r.Connection,
	}
	return lo.OmitByValues(ids, []string{""})
}

type SubjectAccessReviewRequest struct {
	Resource SubjectAccessReviewResource `json:"resource"`
	Action   string                      `json:"action"`

	// Subjects are the ids of the subjects to review. ["*"] reviews every subject the policy grants something to.
	Subjects []string `json:"subjects"`

	// Explain asks for the rules that decide each subject's answer.
	Explain bool `json:"explain,omitempty"`
}

type SubjectAccessReviewResult struct {
	Subject string `json:"subject"`
	Allowed bool   `json:"allowed"`
	Error   string `json:"error,omitempty"`

	// Rules are the rules the subject holds for the action, when explained: those that match the check decide it, and
	// those that don't are for the action but not for this resource. Denies come first.
	Rules []ReviewedRule `json:"rules,omitempty"`

	// Requirements are the other checks the operation makes, e.g. read on a run's target. Allowed only answers the
	// action itself, so the operation is refused when it's allowed and a requirement isn't.
	Requirements []ReviewRequirement `json:"requirements,omitempty"`
}

// ReviewRequirement is another check an operation makes besides its action.
type ReviewRequirement struct {
	Action string `json:"action"`

	// On is the resource type, or the RBAC object, it's checked on.
	On      string `json:"on"`
	Allowed bool   `json:"allowed"`
}

type SubjectAccessReviewResponse struct {
	Resource SubjectAccessReviewResource `json:"resource"`
	Action   string                      `json:"action"`

	// Object is the RBAC object the action was checked on, when it's checked on a whole type rather than a resource,
	// e.g. catalog for update on a config.
	Object string `json:"object,omitempty"`

	// Requires is what an operation checks besides the action, e.g. read on the target.
	Requires string `json:"requires,omitempty"`

	Results []SubjectAccessReviewResult `json:"results"`
}

// ReviewedRule is a rule a subject holds, and how it holds it.
type ReviewedRule struct {
	// Effect is allow, or deny for a rule of a deny binding.
	Effect string `json:"effect"`

	// Matches reports whether the rule matches the check. One that doesn't is for the action, but not for the
	// resource, e.g. the resource is outside its Scopes, its target or its binding's constraint.
	Matches bool `json:"matches"`

	RoleBinding *RoleBindingRef `json:"role_binding,omitempty"`
	Role        *RBACObjectRef  `json:"role,omitempty"`
	Rule        string          `json:"rule,omitempty"`

	// Permission is the legacy Permission that grants the rule.
	Permission *RBACObjectRef `json:"permission,omitempty"`

	// Direct is set when the binding or Permission selects the subject itself.
	Direct bool `json:"direct,omitempty"`

	// Through are the subjects, e.g. teams, through which the subject holds the rule.
	Through []ReviewSubjectRef `json:"through,omitempty"`

	// Hidden is set when the reviewer may not read what grants the rule, so it isn't named.
	Hidden bool `json:"hidden,omitempty"`
}

type RBACObjectRef struct {
	ID        string `json:"id,omitempty"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type RoleBindingRef struct {
	RBACObjectRef `json:",inline"`

	// Constrained is set when the binding narrows its Role with a constraint.
	Constrained bool `json:"constrained,omitempty"`
}

type ReviewSubjectRef struct {
	ID   string `json:"id,omitempty"`
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// reviewCheck is how a review checks the action: on a whole object, or on resources.
type reviewCheck struct {
	action   string
	contract adapter.ActionContract

	// object is the RBAC object the action is checked on. Empty when it's checked on attr.
	object string

	attr *models.ABACAttribute

	// resource and target are the resources the action is checked on, with the kind of each.
	resource, target *membership.Ref

	// onType is set when the action takes a resource but is only checked on its type's object.
	onType bool
}

func (c reviewCheck) checked() any {
	if c.object != "" {
		return c.object
	}
	return c.attr
}

// planReview validates a review's action and resource against the action's contract, and returns how to check it.
func planReview(ctx context.Context, resource SubjectAccessReviewResource, action string) (reviewCheck, error) {
	if action == "" {
		return reviewCheck{}, api.Errorf(api.EINVALID, "action is required")
	}

	contract, err := adapter.ContractFor(action)
	if err != nil {
		return reviewCheck{}, api.Errorf(api.EINVALID, "%v", err)
	}
	check := reviewCheck{action: action, contract: contract}

	ids := resource.ids()
	if resource.Global != "" {
		if len(ids) > 0 {
			return reviewCheck{}, api.Errorf(api.EINVALID, "resource.global can't be combined with another resource")
		}
		// Reviewing access across a type takes read on it (specs/authorization/roles.md, Section 2.3)
		if contract.TakesResource() && !CheckContext(ctx, resource.Global, policy.ActionRead) {
			return reviewCheck{}, api.Errorf(api.EFORBIDDEN, "reviewing %s on all of %s takes read on it", action, resource.Global)
		}
		check.object = resource.Global
		return check, nil
	}

	if !contract.TakesResource() {
		if len(ids) > 0 {
			return reviewCheck{}, api.Errorf(api.EINVALID, "%s takes no resource: it's checked on %s", action, contract.Object)
		}
		check.object = contract.Object
		return check, nil
	}

	primary, target, err := splitReviewResources(contract, ids)
	if err != nil {
		return reviewCheck{}, err
	}

	if check.attr, err = loadReviewResources(ctx, ids); err != nil {
		return reviewCheck{}, err
	}
	check.resource = &membership.Ref{Type: primary, ID: uuid.MustParse(ids[primary])}
	if target != "" {
		check.target = &membership.Ref{Type: target, ID: uuid.MustParse(ids[target])}
	}

	if checkedOnType(contract, primary) {
		if target != "" {
			return reviewCheck{}, api.Errorf(api.EINVALID, "%s takes no target", action)
		}
		check.object, check.onType = adapter.ObjectForType(primary), true
	}

	return check, nil
}

// checkedOnType reports whether the operations check the action on the type's object rather than on each resource:
// create, update and delete, and reading connections (specs/authorization/roles.md, Section 2.2).
func checkedOnType(contract adapter.ActionContract, kind string) bool {
	return contract.CheckedOnObjects() || contract.Action == policy.ActionRead && kind == policy.ResourceConnection
}

// splitReviewResources returns which of the named resources is the action's primary resource, and which its target.
func splitReviewResources(contract adapter.ActionContract, ids map[string]string) (string, string, error) {
	if len(ids) == 0 {
		return "", "", api.Errorf(api.EINVALID, "%s needs a resource: one of %s", contract.Action, strings.Join(reviewableTypes(contract.Resources), ", "))
	}

	var primaries, targets, others []string
	for _, kind := range lo.Keys(ids) {
		switch {
		case slices.Contains(contract.Resources, kind):
			primaries = append(primaries, kind)
		case slices.Contains(contract.Targets, kind):
			targets = append(targets, kind)
		default:
			others = append(others, kind)
		}
	}
	slices.Sort(primaries)
	slices.Sort(targets)
	slices.Sort(others)

	if len(others) > 0 {
		return "", "", api.Errorf(api.EINVALID, "%s doesn't take a %s", contract.Action, strings.Join(others, " or "))
	}

	// An action whose resources may also be targets, e.g. read on a config, takes a single resource
	if len(primaries) == 2 && len(targets) == 0 && len(contract.Targets) > 0 {
		for i, kind := range primaries {
			if slices.Contains(contract.Targets, kind) {
				primaries, targets = []string{primaries[1-i]}, []string{kind}
				break
			}
		}
	}

	switch {
	case len(primaries) != 1:
		return "", "", api.Errorf(api.EINVALID, "%s needs exactly one resource: one of %s", contract.Action, strings.Join(reviewableTypes(contract.Resources), ", "))
	case len(targets) > 1:
		return "", "", api.Errorf(api.EINVALID, "%s takes at most one target", contract.Action)
	case len(targets) == 1:
		return primaries[0], targets[0], nil
	}
	return primaries[0], "", nil
}

// reviewableTypes are the resource types of those given that a review can name.
func reviewableTypes(kinds []string) []string {
	known := lo.Keys(SubjectAccessReviewResource{Playbook: "-", Config: "-", Component: "-", Check: "-", View: "-", Connection: "-"}.ids())
	out := lo.Filter(kinds, func(kind string, _ int) bool { return slices.Contains(known, kind) })
	slices.Sort(out)
	return out
}

// loadReviewResources loads the resources a review names, in a fixed order. A resource that doesn't exist and one the
// reviewer may not read are not found alike, and nothing after it is looked up, so a review never tells the reviewer
// about a resource they can't see (specs/authorization/roles.md, Section 2.3).
func loadReviewResources(ctx context.Context, ids map[string]string) (*models.ABACAttribute, error) {
	attr := &models.ABACAttribute{}
	kinds := lo.Keys(ids)
	slices.Sort(kinds)
	for _, kind := range kinds {
		raw := ids[kind]
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "resource.%s must be a valid UUID", kind)
		}

		var dest any
		switch kind {
		case policy.ResourcePlaybook:
			dest = &attr.Playbook
		case policy.ResourceConfig:
			dest = &attr.Config
		case policy.ResourceComponent:
			dest = &attr.Component
		case policy.ResourceCheck:
			dest = &attr.Check
		case policy.ResourceView:
			dest = &attr.View
		case policy.ResourceConnection:
			dest = &attr.Connection
		}

		err = ctx.DB().Where("id = ? AND deleted_at IS NULL", id).First(dest).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ctx.Oops().Wrapf(err, "failed to resolve %s %q", kind, raw)
		} else if err != nil || !readableByReviewer(ctx, kind, attr) {
			return nil, api.Errorf(api.ENOTFOUND, "%s %q not found", kind, raw)
		}
	}
	return attr, nil
}

// readableByReviewer reports whether the reviewer may read the resource of the type, as its own read checks do.
func readableByReviewer(ctx context.Context, kind string, attr *models.ABACAttribute) bool {
	if kind == policy.ResourceConnection {
		// Connections are read as a whole type (specs/authorization/roles.md, Section 2.2)
		return CheckContext(ctx, policy.ObjectConnection, policy.ActionRead)
	}

	only := &models.ABACAttribute{}
	switch kind {
	case policy.ResourcePlaybook:
		only.Playbook = attr.Playbook
	case policy.ResourceConfig:
		only.Config = attr.Config
	case policy.ResourceComponent:
		only.Component = attr.Component
	case policy.ResourceCheck:
		only.Check = attr.Check
	case policy.ResourceView:
		only.View = attr.View
	}
	return HasPermission(ctx, ctx.Subject(), only, policy.ActionRead)
}

// RunSubjectAccessReview checks what each subject may do: the action on the resource, as a request of the subject
// would be checked. Access tokens aren't subjects (specs/authorization/token-bindings.md).
func RunSubjectAccessReview(ctx context.Context, req SubjectAccessReviewRequest) (SubjectAccessReviewResponse, error) {
	check, err := planReview(ctx, req.Resource, req.Action)
	if err != nil {
		return SubjectAccessReviewResponse{}, err
	}

	subjects, err := resolveReviewSubjects(ctx, req.Subjects)
	if err != nil {
		return SubjectAccessReviewResponse{}, err
	} else if req.Explain && len(subjects) > MaxExplainedSubjects {
		return SubjectAccessReviewResponse{}, api.Errorf(api.EINVALID, "a review explains at most %d subjects", MaxExplainedSubjects)
	}

	if check.attr != nil {
		// Every subject is checked against the same moment of the resources' Scope membership
		if ctx, err = membership.ForOperation(ctx, dutyRBAC.MembershipRefs(check.attr)...); err != nil {
			return SubjectAccessReviewResponse{}, ctx.Oops().Wrapf(err, "failed to read the scope membership of the resource")
		}
	}

	// Every subject's answer, requirements and explanation come from the same moment of the policy, so they agree
	snapshot, err := dutyRBAC.SnapshotPolicy()
	if err != nil {
		return SubjectAccessReviewResponse{}, ctx.Oops().Wrap(err)
	}

	var names *ruleNames
	if req.Explain {
		names = newRuleNames(ctx)
	}

	response := SubjectAccessReviewResponse{
		Resource: req.Resource,
		Action:   req.Action,
		Requires: check.contract.Requires,
		Results:  make([]SubjectAccessReviewResult, 0, len(subjects)),
	}
	if check.onType {
		response.Object = check.object
	}

	for _, subject := range subjects {
		result := SubjectAccessReviewResult{Subject: subject}

		explanation, err := snapshot.Explain(ctx, subject, check.checked(), check.action)
		if err != nil {
			return SubjectAccessReviewResponse{}, ctx.Oops().Wrapf(err, "failed to review %s for %s", check.action, subject)
		}
		result.Allowed = explanation.Allowed

		if result.Requirements, err = reviewRequirements(ctx, snapshot, subject, check); err != nil {
			return SubjectAccessReviewResponse{}, err
		}

		if names != nil {
			if result.Rules, err = explainReview(names, explanation.Rules); err != nil {
				return SubjectAccessReviewResponse{}, err
			}
		}

		response.Results = append(response.Results, result)
	}

	return response, nil
}

// reviewRequirements evaluates what the operation checks besides its action (ActionContract.Requires), as the
// operations do: read on the target of a run, an approval or a cancellation, read on the config a plugin operation is
// invoked on, and mcp:use for mcp:run. A run started through MCP is then checked as any run, when it starts.
func reviewRequirements(ctx context.Context, snapshot *dutyRBAC.PolicySnapshot, subject string, check reviewCheck) ([]ReviewRequirement, error) {
	requirement := func(object any, action, on string) (ReviewRequirement, error) {
		explanation, err := snapshot.Explain(ctx, subject, object, action)
		if err != nil {
			return ReviewRequirement{}, ctx.Oops().Wrapf(err, "failed to review %s on %s for %s", action, on, subject)
		}
		return ReviewRequirement{Action: action, On: on, Allowed: explanation.Allowed}, nil
	}

	var object any
	var action, on string
	switch {
	case check.action == policy.ActionMCPRun:
		object, action, on = policy.ObjectMCP, policy.ActionMCPUse, policy.ObjectMCP
	case check.attr == nil:
		return nil, nil
	case slices.Contains(targetReadActions, check.action) && check.target != nil:
		object = &models.ABACAttribute{Config: check.attr.Config, Component: check.attr.Component, Check: check.attr.Check}
		action, on = policy.ActionRead, check.target.Type
	case strings.HasPrefix(check.action, policy.ActionPluginInvokePrefix):
		object, action, on = &models.ABACAttribute{Config: check.attr.Config}, policy.ActionRead, policy.ResourceConfig
	default:
		return nil, nil
	}

	r, err := requirement(object, action, on)
	if err != nil {
		return nil, err
	}
	return []ReviewRequirement{r}, nil
}

// targetReadActions are the actions whose operations also need read on their target, when there is one.
var targetReadActions = []string{policy.ActionPlaybookRun, policy.ActionPlaybookApprove, policy.ActionPlaybookCancel}

// resolveReviewSubjects returns the subjects a review names. ["*"] names every person, team and playbook the policy
// grants something to, directly or through a team, that the reviewer may read: a subject with no grant may do nothing.
func resolveReviewSubjects(ctx context.Context, subjects []string) ([]string, error) {
	if len(subjects) == 0 {
		return nil, api.Errorf(api.EINVALID, "at least one subject is required")
	}

	if len(subjects) == 1 && strings.TrimSpace(subjects[0]) == "*" {
		all, err := grantedSubjects(ctx)
		if err != nil {
			return nil, err
		}
		subjects = all
	} else {
		subjects = lo.Map(subjects, func(s string, _ int) string { return strings.TrimSpace(s) })
	}

	if len(subjects) > MaxSubjectAccessReviewSubjects {
		return nil, api.Errorf(api.EINVALID, "subjects exceeds maximum of %d", MaxSubjectAccessReviewSubjects)
	}

	for _, subject := range subjects {
		if subject == "" {
			return nil, api.Errorf(api.EINVALID, "subject is required")
		} else if !reviewableSubject(subject) {
			return nil, api.Errorf(api.EINVALID, "%q isn't a subject", subject)
		}
	}

	return subjects, nil
}

// reviewableSubject reports whether a review may be run as the subject. Bindings, access tokens and users of external
// identity providers aren't subjects. An access token's limit is filed under its bindings, and isn't for anyone allowed
// to review access to probe (specs/authorization/token-bindings.md).
func reviewableSubject(subject string) bool {
	return !strings.HasPrefix(subject, models.AccessTokenPrincipalPrefix) &&
		!strings.HasPrefix(subject, models.BindingPrincipalPrefix) &&
		!strings.HasPrefix(subject, models.FederatedPrincipalPrefix)
}

// grantedSubjects returns the people, teams and playbooks the policy holds a grant or a membership for, that the
// reviewer may read, sorted.
func grantedSubjects(ctx context.Context) ([]string, error) {
	snapshot, err := dutyRBAC.SnapshotPolicy()
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	ids := lo.FilterMap(snapshot.Principals(), func(s string, _ int) (uuid.UUID, bool) {
		id, err := uuid.Parse(s)
		return id, err == nil
	})
	if len(ids) == 0 {
		return nil, nil
	}

	var subjects []string
	if CheckContext(ctx, adapter.ObjectForType(v1.ResourcePerson), policy.ActionRead) {
		var people []string
		if err := ctx.DB().Model(&models.Person{}).Where("id IN ? AND deleted_at IS NULL", ids).Pluck("id::text", &people).Error; err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to list people")
		}
		subjects = append(subjects, people...)
	}
	if CheckContext(ctx, adapter.ObjectForType(v1.ResourceTeam), policy.ActionRead) {
		var teams []string
		if err := ctx.DB().Model(&models.Team{}).Where("id IN ? AND deleted_at IS NULL", ids).Pluck("id::text", &teams).Error; err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to list teams")
		}
		subjects = append(subjects, teams...)
	}

	var playbooks []models.Playbook
	if err := ctx.DB().Where("id IN ? AND deleted_at IS NULL", ids).Find(&playbooks).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to list playbooks")
	}
	for _, playbook := range playbooks {
		if readableByReviewer(ctx, policy.ResourcePlaybook, &models.ABACAttribute{Playbook: playbook}) {
			subjects = append(subjects, playbook.ID.String())
		}
	}

	slices.Sort(subjects)
	return slices.Compact(subjects), nil
}

// explainReview names the rules that decide a review for the reviewer.
func explainReview(names *ruleNames, explained []dutyRBAC.ExplainedRule) ([]ReviewedRule, error) {
	// A rule can compile to several policies, e.g. a read on a Scope and on the whole type it selects
	type key struct{ principal, id string }
	merged := map[key]*dutyRBAC.ExplainedRule{}
	var order []key
	for _, rule := range explained {
		k := key{rule.Principal, rule.ID}
		if existing, ok := merged[k]; ok {
			existing.Matches = existing.Matches || rule.Matches
			continue
		}
		merged[k] = &rule
		order = append(order, k)
	}

	rules := make([]ReviewedRule, 0, len(order))
	for _, k := range order {
		reviewed, err := names.describe(*merged[k])
		if err != nil {
			return nil, err
		}
		rules = append(rules, reviewed)
	}

	// The rules that decide the check first, denies before allows
	slices.SortStableFunc(rules, func(a, b ReviewedRule) int {
		if a.Matches != b.Matches {
			return lo.Ternary(a.Matches, -1, 1)
		}
		if c := cmp.Compare(b.Effect, a.Effect); c != 0 {
			return c
		}
		return cmp.Compare(a.sortKey(), b.sortKey())
	})
	return rules, nil
}

func (r ReviewedRule) sortKey() string {
	switch {
	case r.RoleBinding != nil:
		return r.RoleBinding.Namespace + "/" + r.RoleBinding.Name + "/" + r.Rule
	case r.Permission != nil:
		return r.Permission.Namespace + "/" + r.Permission.Name
	}
	return ""
}

// ruleNames names the bindings, roles, Permissions and teams that grant rules, as far as the reviewer may read them:
// an access review never names what the reviewer can't see (specs/authorization/roles.md, Section 2.3).
// It caches what it loads for the review.
type ruleNames struct {
	ctx context.Context

	canReadBindings, canReadRoles, canReadTeams bool

	bindings    map[uuid.UUID]*models.RoleBinding
	roles       map[string]*models.Role
	permissions map[uuid.UUID]*models.Permission
	teams       map[string]*models.Team
}

func newRuleNames(ctx context.Context) *ruleNames {
	return &ruleNames{
		ctx:             ctx,
		canReadBindings: CheckContext(ctx, adapter.ObjectForType(v1.ResourceRoleBinding), policy.ActionRead),
		canReadRoles:    CheckContext(ctx, adapter.ObjectForType(v1.ResourceRole), policy.ActionRead),
		canReadTeams:    CheckContext(ctx, adapter.ObjectForType(v1.ResourceTeam), policy.ActionRead),
		bindings:        map[uuid.UUID]*models.RoleBinding{},
		roles:           map[string]*models.Role{},
		permissions:     map[uuid.UUID]*models.Permission{},
		teams:           map[string]*models.Team{},
	}
}

func (n *ruleNames) describe(rule dutyRBAC.ExplainedRule) (ReviewedRule, error) {
	reviewed := ReviewedRule{Effect: rule.Effect, Matches: rule.Matches}

	if len(rule.Paths) == 0 {
		reviewed.Direct = true
	}
	for _, path := range rule.Paths {
		if len(path) == 1 {
			reviewed.Direct = true
			continue
		}
		for _, through := range path[:len(path)-1] {
			ref, err := n.subject(through)
			if err != nil {
				return ReviewedRule{}, err
			}
			if !slices.Contains(reviewed.Through, ref) {
				reviewed.Through = append(reviewed.Through, ref)
			}
		}
	}

	if bindingID, ruleName, ok := adapter.ParseBindingRuleID(rule.ID); ok {
		if !n.canReadBindings {
			reviewed.Hidden = true
			return reviewed, nil
		}

		binding, err := n.binding(bindingID)
		if err != nil || binding == nil {
			return reviewed, err
		}

		reviewed.Rule = ruleName
		reviewed.RoleBinding = &RoleBindingRef{
			RBACObjectRef: RBACObjectRef{ID: binding.ID.String(), Namespace: binding.Namespace, Name: binding.Name},
			Constrained:   binding.Constraint != nil,
		}

		// The Role, and the rule's name in it, are named only to a reviewer who may read Roles
		if !n.canReadRoles {
			reviewed.Rule = ""
		} else if namespace, name, err := adapter.BindingRoleRef(*binding); err == nil {
			reviewed.Role = &RBACObjectRef{Namespace: namespace, Name: name}
			if role, err := n.role(namespace, name); err != nil {
				return ReviewedRule{}, err
			} else if role != nil {
				reviewed.Role.ID = role.ID.String()
			}
		}
		return reviewed, nil
	}

	if permissionID, err := uuid.Parse(rule.ID); err == nil {
		// Permissions are deprecated RoleBindings (specs/authorization/permissions.md), named to whoever may read those
		if !n.canReadBindings {
			reviewed.Hidden = true
			return reviewed, nil
		}

		permission, err := n.permission(permissionID)
		if err != nil {
			return ReviewedRule{}, err
		} else if permission != nil {
			reviewed.Permission = &RBACObjectRef{ID: permission.ID.String(), Namespace: permission.Namespace, Name: permission.Name}
		}
	}

	return reviewed, nil
}

// subject returns a subject a rule is held through, e.g. a team.
func (n *ruleNames) subject(principal string) (ReviewSubjectRef, error) {
	if plugin, ok := strings.CutPrefix(principal, "plugin:"); ok {
		return ReviewSubjectRef{ID: principal, Type: "plugin", Name: plugin}, nil
	}

	if _, err := uuid.Parse(principal); err == nil {
		team, err := n.team(principal)
		if err != nil {
			return ReviewSubjectRef{}, err
		} else if team != nil {
			if !n.canReadTeams {
				return ReviewSubjectRef{Type: "team"}, nil
			}
			return ReviewSubjectRef{ID: principal, Type: "team", Name: team.Name}, nil
		}
	}

	return ReviewSubjectRef{ID: principal, Type: "other"}, nil
}

func (n *ruleNames) binding(id uuid.UUID) (*models.RoleBinding, error) {
	if binding, ok := n.bindings[id]; ok {
		return binding, nil
	}

	var binding models.RoleBinding
	if err := n.ctx.DB().Where("id = ? AND deleted_at IS NULL", id).Limit(1).Find(&binding).Error; err != nil {
		return nil, n.ctx.Oops().Wrapf(err, "failed to load role binding %s", id)
	}

	n.bindings[id] = lo.Ternary(binding.ID == uuid.Nil, nil, &binding)
	return n.bindings[id], nil
}

func (n *ruleNames) role(namespace, name string) (*models.Role, error) {
	key := namespace + "/" + name
	if role, ok := n.roles[key]; ok {
		return role, nil
	}

	var role models.Role
	if err := n.ctx.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", namespace, name).Limit(1).Find(&role).Error; err != nil {
		return nil, n.ctx.Oops().Wrapf(err, "failed to load role %s", key)
	}

	n.roles[key] = lo.Ternary(role.ID == uuid.Nil, nil, &role)
	return n.roles[key], nil
}

func (n *ruleNames) permission(id uuid.UUID) (*models.Permission, error) {
	if permission, ok := n.permissions[id]; ok {
		return permission, nil
	}

	var permission models.Permission
	if err := n.ctx.DB().Where("id = ? AND deleted_at IS NULL", id).Limit(1).Find(&permission).Error; err != nil {
		return nil, n.ctx.Oops().Wrapf(err, "failed to load permission %s", id)
	}

	n.permissions[id] = lo.Ternary(permission.ID == uuid.Nil, nil, &permission)
	return n.permissions[id], nil
}

func (n *ruleNames) team(id string) (*models.Team, error) {
	if team, ok := n.teams[id]; ok {
		return team, nil
	}

	var team models.Team
	if err := n.ctx.DB().Where("id = ? AND deleted_at IS NULL", id).Limit(1).Find(&team).Error; err != nil {
		return nil, n.ctx.Oops().Wrapf(err, "failed to load team %s", id)
	}

	n.teams[id] = lo.Ternary(team.ID == uuid.Nil, nil, &team)
	return n.teams[id], nil
}
