package rbac

import (
	"fmt"
	"sync"

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

	// Mission Control reloads policy with its RLS caches. Duty's automatic reload doesn't,
	// and Casbin's decision cache can retain allows computed during a policy swap.
	rbac.Enforcer().StopAutoLoadPolicy()
	rbac.Enforcer().EnableCache(false)

	// duty's Init adds its built-in roles' rules to the loaded policy. The adapter doesn't store them, so loading
	// the policy again leaves them out.
	return ReloadPolicy()
}

// FlushPolicyCaches includes authentication-owned RLS caches when auth is initialized.
var FlushPolicyCaches = FlushAccessSummaries

var policyReload sync.Mutex

// ReloadPolicy revokes stale grants even when loading fails, and invalidates derived authorization caches.
func ReloadPolicy() error {
	policyReload.Lock()
	defer policyReload.Unlock()
	defer FlushPolicyCaches()
	if err := rbac.ReloadPolicy(); err != nil {
		// Casbin preserves the old model on load errors, including revoked email grants.
		if enforcer := rbac.Enforcer(); enforcer != nil {
			enforcer.ClearPolicy()
		}
		return err
	}
	return nil
}
