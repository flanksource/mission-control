package adapter

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/samber/lo"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// enforcement is how Mission Control checks an action, which limits the selectors its rules can use.
type enforcement string

const (
	// enforcedPerRequest actions are checked by casbin on the resources of each request.
	enforcedPerRequest enforcement = "request"

	// enforcedWithRows actions are also enforced on listings, by row-level security over stored Scope membership.
	enforcedWithRows enforcement = "rows"

	// enforcedOnObjects actions are only checked on whole object types (e.g. catalog), never on individual resources.
	enforcedOnObjects enforcement = "objects"

	// enforcedWithoutResource actions aren't performed on a resource: they're checked on the contract's Object alone.
	enforcedWithoutResource enforcement = "none"
)

// ActionConnectionUse allows acting with a connection's credentials, e.g. sending a notification through it or testing
// it (see specs/authorization/roles.md, Section 2.2). Reading a connection takes read instead.
const ActionConnectionUse = "connection:use"

// Actions that take no resource (see specs/authorization/roles.md, Section 2.3).
// mcp:use, property:manage, event:read and job:read are defined in duty's policy package.
const (
	// ActionPersonManage allows inviting, disabling, re-enabling and deleting accounts, and changing their
	// properties, name or email. Checked on policy.ObjectPeople.
	ActionPersonManage = "person:manage"

	// ActionAccessReview allows asking what any subject may do: subject access reviews and searches.
	// Checked on policy.ObjectRBAC.
	ActionAccessReview = "access:review"
)

// ActionContract declares the inputs an action accepts.
type ActionContract struct {
	Action string

	// Resources are the resource types the action accepts as its primary resource.
	Resources []string

	// Targets are the resource types the action accepts as an optional target.
	// Empty when the action doesn't take a target.
	Targets []string

	// Requires describes what else an operation checks, e.g. read on the target.
	Requires string

	// Object is the RBAC object an action without a resource is checked on.
	// Empty for every action that takes a resource.
	Object string

	enforcement enforcement
}

// ObjectChecks is the RBAC object a whole-type read of checks is granted on.
// Built-in roles check the checks table against the canaries object, so a grant on canaries
// would also list every check: bindings get an object of their own for checks.
const ObjectChecks = "checks"

// RBAC objects that whole-type grants of a type compile to, where duty's object for the type is shared with another type.
// duty's policy package doesn't define them yet.
const (
	ObjectNotificationSilences = "notification-silences"
	ObjectTeams                = "teams"
	ObjectScopes               = "scopes"
	ObjectRoles                = "roles"
	ObjectRoleBindings         = "role-bindings"

	// ObjectScrapers is what whole-type grants on scrapers compile to. duty files config_scrapers under
	// database.config_scrapers, which logging backends share.
	ObjectScrapers = "scrapers"
)

// objectForType is the RBAC object of each resource type that can be checked on whole types.
// Every type has its own object, so a grant on one type never grants another.
var objectForType = map[string]string{
	policy.ResourceConfig:     policy.ObjectCatalog,
	policy.ResourceComponent:  policy.ObjectTopology,
	policy.ResourceCheck:      ObjectChecks,
	policy.ResourceCanary:     policy.ObjectCanary,
	policy.ResourcePlaybook:   policy.ObjectPlaybooks,
	policy.ResourceConnection: policy.ObjectConnection,
	policy.ResourceScraper:    ObjectScrapers,

	v1.ResourceApplication:         policy.ObjectApplication,
	v1.ResourceNotification:        policy.ObjectNotification,
	v1.ResourceNotificationSilence: ObjectNotificationSilences,
	v1.ResourceAgent:               policy.ObjectAgent,
	v1.ResourcePerson:              policy.ObjectPeople,
	v1.ResourceTeam:                ObjectTeams,
	v1.ResourceScope:               ObjectScopes,
	v1.ResourceRole:                ObjectRoles,
	v1.ResourceRoleBinding:         ObjectRoleBindings,
}

var (
	// genericResources are what create, update and delete take: every type but checks, people and views.
	genericResources = append([]string{policy.ResourceConfig, policy.ResourceComponent, policy.ResourceCanary, policy.ResourcePlaybook, policy.ResourceConnection, policy.ResourceScraper},
		lo.Without(v1.WholeTypeResources, v1.ResourcePerson)...)

	// readableResources are what read takes: every type but views.
	readableResources = append([]string{policy.ResourceConfig, policy.ResourceComponent, policy.ResourceCheck, policy.ResourceCanary, policy.ResourcePlaybook, policy.ResourceConnection, policy.ResourceScraper},
		v1.WholeTypeResources...)

	playbookTargets = []string{policy.ResourceConfig, policy.ResourceComponent, policy.ResourceCheck}
)

var contracts = map[string]ActionContract{
	policy.ActionRead: {Resources: readableResources, enforcement: enforcedWithRows},

	policy.ActionCreate: {Resources: genericResources, enforcement: enforcedOnObjects},
	policy.ActionUpdate: {Resources: genericResources, enforcement: enforcedOnObjects},
	policy.ActionDelete: {Resources: genericResources, enforcement: enforcedOnObjects},

	policy.ActionPlaybookRun: {
		Resources: []string{policy.ResourcePlaybook}, Targets: playbookTargets, enforcement: enforcedPerRequest,
		Requires: "read on the target, when there is one",
	},
	policy.ActionPlaybookApprove: {
		Resources: []string{policy.ResourcePlaybook}, Targets: playbookTargets, enforcement: enforcedPerRequest,
	},
	policy.ActionPlaybookCancel: {
		Resources: []string{policy.ResourcePlaybook}, Targets: playbookTargets, enforcement: enforcedPerRequest,
	},
	policy.ActionMCPRun: {
		Resources: []string{policy.ResourcePlaybook}, enforcement: enforcedPerRequest,
		Requires: "mcp:use; the run itself also needs playbook:run",
	},

	// Connections are checked per request, on their stored Scope membership, so unlike read any connection selector works.
	ActionConnectionUse: {Resources: []string{policy.ResourceConnection}, enforcement: enforcedPerRequest},

	ActionPersonManage:          {Object: policy.ObjectPeople, enforcement: enforcedWithoutResource},
	ActionAccessReview:          {Object: policy.ObjectRBAC, enforcement: enforcedWithoutResource},
	policy.ActionMCPUse:         {Object: policy.ObjectMCP, enforcement: enforcedWithoutResource},
	policy.ActionPropertyManage: {Object: policy.ObjectProperties, enforcement: enforcedWithoutResource},
	policy.ActionEventRead:      {Object: policy.ObjectEvents, enforcement: enforcedWithoutResource},
	policy.ActionJobRead:        {Object: policy.ObjectJobs, enforcement: enforcedWithoutResource},
}

// Actions returns every action with a contract, sorted. Plugin actions aren't listed: there's one per plugin operation.
func Actions() []string {
	actions := slices.Collect(maps.Keys(contracts))
	slices.Sort(actions)
	return actions
}

// ObjectForType returns the object used to enforce whole-type grants, or empty for an unknown type.
func ObjectForType(resourceType string) string {
	return objectForType[resourceType]
}

// ContractFor returns the contract of an action.
// Unknown actions and patterns have none.
func ContractFor(action string) (ActionContract, error) {
	if contract, ok := contracts[action]; ok {
		contract.Action = action
		return contract, nil
	}

	if rest, found := strings.CutPrefix(action, policy.ActionPluginInvokePrefix); found {
		plugin, name, found := strings.Cut(rest, ":")
		if !found || plugin == "" || name == "" || strings.ContainsAny(rest, "*!, ") || strings.Contains(name, ":") {
			return ActionContract{}, fmt.Errorf("action %q must be %s<plugin>:<operation>, without patterns", action, policy.ActionPluginInvokePrefix)
		}

		return ActionContract{
			Action: action, Resources: []string{policy.ResourceConfig}, enforcement: enforcedPerRequest,
			Requires: "read on the config",
		}, nil
	}

	return ActionContract{}, fmt.Errorf("unknown action %q", action)
}

// TakesResource reports whether the action is performed on a resource, so its rules need a resource Scope.
func (c ActionContract) TakesResource() bool {
	return c.enforcement != enforcedWithoutResource
}

// validateInput checks that a Scope can fill an input ("resource" or "target") of the action:
// every type it selects is accepted, and every selector can be enforced as written.
func (c ActionContract) validateInput(input string, scope ScopeSelection) error {
	accepted := c.Resources
	if input == "target" {
		accepted = c.Targets
	}

	for _, kind := range scope.DeclaredTypes() {
		if !slices.Contains(accepted, kind) {
			if len(accepted) == 0 {
				return fmt.Errorf("%s doesn't take a %s", c.Action, input)
			}
			return fmt.Errorf("%s's %s accepts %s; scope %s selects %s",
				c.Action, input, strings.Join(accepted, ", "), scope.Name, strings.Join(scope.DeclaredTypes(), ", "))
		}

		for _, selector := range scope.Selectors[kind] {
			if err := c.enforceable(kind, selector); err != nil {
				return fmt.Errorf("scope %s: %w", scope.Name, err)
			}
		}
	}

	return nil
}

// enforceable checks that the selector can be enforced as written wherever the action is checked.
func (c ActionContract) enforceable(kind string, selector types.ResourceSelector) error {
	switch c.enforcement {
	case enforcedOnObjects:
		if !selector.Wildcard() {
			return fmt.Errorf(`%s is only checked on whole types, so its %s selectors must be exactly name: "*"`, c.Action, kind)
		}
	case enforcedWithRows:
		// Membership is decided by the resource alone, so listings can filter by any Scope.
		// Connections aren't filtered by row.
		if kind == policy.ResourceConnection && !selector.Wildcard() {
			return fmt.Errorf(`connections aren't filtered by row, so %s can only select them with name: "*"`, c.Action)
		}
	}

	return nil
}
