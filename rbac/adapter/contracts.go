package adapter

import (
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
)

// enforcement is how Mission Control checks an action, which limits the selectors its rules can use.
type enforcement string

const (
	// enforcedPerRequest actions are checked by casbin on the resources of each request.
	enforcedPerRequest enforcement = "request"

	// enforcedWithRows actions are also enforced by row filters in Postgres, which support fewer selectors.
	enforcedWithRows enforcement = "rows"

	// enforcedOnObjects actions are only checked on whole object types (e.g. catalog), never on individual resources.
	enforcedOnObjects enforcement = "objects"
)

// ActionContract declares the inputs an action accepts.
type ActionContract struct {
	Action string

	// Resources are the resource types the action accepts as its primary resource.
	Resources []string

	// Targets are the resource types the action accepts as an optional target.
	// Empty when the action doesn't take a target.
	Targets []string

	// AllowsDeny reports whether deny rules on the action can be enforced.
	AllowsDeny bool

	// Requires describes what else an operation checks, e.g. read on the target.
	Requires string

	enforcement enforcement
}

// ObjectChecks is the RBAC object a whole-type read of checks is granted on.
// Built-in roles check the checks table against the canaries object, so a grant on canaries
// would also list every check: bindings get an object of their own for checks.
const ObjectChecks = "checks"

// objectForType is the RBAC object of each resource type that can be checked on whole types.
var objectForType = map[string]string{
	policy.ResourceConfig:     policy.ObjectCatalog,
	policy.ResourceComponent:  policy.ObjectTopology,
	policy.ResourceCheck:      ObjectChecks,
	policy.ResourceCanary:     policy.ObjectCanary,
	policy.ResourcePlaybook:   policy.ObjectPlaybooks,
	policy.ResourceConnection: policy.ObjectConnection,
}

var (
	genericResources  = []string{policy.ResourceConfig, policy.ResourceComponent, policy.ResourceCanary, policy.ResourcePlaybook, policy.ResourceConnection}
	readableResources = []string{policy.ResourceConfig, policy.ResourceComponent, policy.ResourceCheck, policy.ResourceCanary, policy.ResourcePlaybook, policy.ResourceConnection}
	playbookTargets   = []string{policy.ResourceConfig, policy.ResourceComponent, policy.ResourceCheck}
)

var contracts = map[string]ActionContract{
	// Denying reads isn't supported yet: the viewer shortcut, and row filters being off for anyone who isn't a guest, would bypass it.
	policy.ActionRead: {Resources: readableResources, enforcement: enforcedWithRows},

	policy.ActionCreate: {Resources: genericResources, AllowsDeny: true, enforcement: enforcedOnObjects},
	policy.ActionUpdate: {Resources: genericResources, AllowsDeny: true, enforcement: enforcedOnObjects},
	policy.ActionDelete: {Resources: genericResources, AllowsDeny: true, enforcement: enforcedOnObjects},

	policy.ActionPlaybookRun: {
		Resources: []string{policy.ResourcePlaybook}, Targets: playbookTargets, AllowsDeny: true, enforcement: enforcedPerRequest,
		Requires: "read on the target, when there is one",
	},
	policy.ActionPlaybookApprove: {
		Resources: []string{policy.ResourcePlaybook}, Targets: playbookTargets, AllowsDeny: true, enforcement: enforcedPerRequest,
	},
	policy.ActionPlaybookCancel: {
		Resources: []string{policy.ResourcePlaybook}, Targets: playbookTargets, AllowsDeny: true, enforcement: enforcedPerRequest,
	},
	policy.ActionMCPRun: {
		Resources: []string{policy.ResourcePlaybook}, AllowsDeny: true, enforcement: enforcedPerRequest,
		Requires: "mcp:use; the run itself also needs playbook:run",
	},
}

// ContractFor returns the contract of an action.
// Unknown actions, patterns, and actions that aren't performed on a resource (e.g. mcp:use) have none.
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
			Action: action, Resources: []string{policy.ResourceConfig}, AllowsDeny: true, enforcement: enforcedPerRequest,
			Requires: "read on the config",
		}, nil
	}

	if action == policy.ActionMCPUse {
		return ActionContract{}, fmt.Errorf("%s isn't performed on a resource, so a role can't grant it", action)
	}

	return ActionContract{}, fmt.Errorf("unknown action %q", action)
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
		if _, err := RowFilter(kind, selector); err != nil {
			return fmt.Errorf("%s selector can't be enforced by row filters: %w", kind, err)
		}
	}

	return nil
}

// requiresRowLevelSecurity reports whether the Scope, filling an input of the action, needs row-level security:
// the action's listings are filtered by row, and the Scope has a target that isn't a whole-type target.
func (c ActionContract) requiresRowLevelSecurity(scope ScopeSelection) bool {
	if c.enforcement != enforcedWithRows {
		return false
	}

	for _, selectors := range scope.Selectors {
		if slices.ContainsFunc(selectors, func(s types.ResourceSelector) bool { return !s.Wildcard() }) {
			return true
		}
	}
	return false
}
