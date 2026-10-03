# Materialised Scope Membership

Implements `scopes.md` §4.3 and §7.1, `roles.md` §3.1.

## Problem

A `read` Scope is evaluated twice: in Go for one resource, and in plpgsql per row for listings. The two must be kept in step by hand, every filtered listing scans the table, and nothing stored says what a Scope selects.

## Design

Evaluate each Scope once per (Scope version, resource), in Go, and store the result. Listings and single-resource `read` checks both read the stored rows; neither evaluates a selector.

```sql
scope_generations (scope_id, generation, targets, state)   -- building | active | retired
scope_members     (scope_id, generation, resource_type, resource_id)
scope_pending     (resource_type, resource_id, seq)        -- filled by triggers
```

- **Scope edited:** build a new generation beside the active one, then switch atomically. Invalid or deleted: retire every generation at once.
- **Resource changed:** a trigger queues it; a worker re-evaluates it every 5 s. Until then the policy hides it.
- **Ordering:** the build, the worker and the switch take a per-type advisory lock, so a stale read can't overwrite a newer decision.
- **Whole-type targets** aren't stored. They grant `all`, and drop out of a constrained grant.

## Claim

Per type: `all`, or grants, each a set of `{scope, generation}` the row must be in all of. A grant is never empty and never repeats a Scope. A grant naming a Scope with no active generation is dropped whole.

## Open questions

- Dense grants: is `id IN (members)` or a per-row `EXISTS` faster? Measure.
- Compile legacy Permission selectors into hidden Scopes, or keep `match_scope()` for them until Permissions go?
- Drain interval and batch size: measure at the largest tenant.
