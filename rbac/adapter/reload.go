package adapter

import (
	dutyRBAC "github.com/flanksource/duty/rbac"
)

// ReloadPolicy loads the policy from the database, and then forgets every decision cached before it's in place.
//
// Casbin clears its decision cache before it loads the policy, and keeps answering checks with the old policy while
// it loads. A check made during the load caches the old answer for casbin.cache.expiry, so a revoked grant could go
// on allowing, or a new one denying, for that long after the reload.
func ReloadPolicy() error {
	if err := dutyRBAC.ReloadPolicy(); err != nil {
		return err
	}
	if enforcer := dutyRBAC.Enforcer(); enforcer != nil {
		return enforcer.InvalidateCache()
	}
	return nil
}
