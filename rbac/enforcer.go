package rbac

import (
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

// Init initialises the enforcer with Mission Control's adapter, with no built-in roles
// (specs/authorization/overview.md, "No built-in roles"). Their stored rules and assignments, e.g. a person's
// admin role, are deleted rather than converted, so nobody has access until a RoleBinding selects them.
func Init(ctx context.Context) error {
	if err := adapter.DeleteBuiltInRoleRules(ctx); err != nil {
		return fmt.Errorf("failed to delete the rules of built-in roles: %w", err)
	}

	if err := rbac.Init(ctx, nil, adapter.NewPermissionAdapter); err != nil {
		return err
	}

	// duty's Init adds its built-in roles' rules to the loaded policy. The adapter doesn't store them, so loading
	// the policy again leaves them out.
	return rbac.ReloadPolicy()
}
