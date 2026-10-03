# Materialised Scope Membership

Implements `scopes.md` §4.3 and §7.1, `roles.md` §3.1.

Each Scope is evaluated once per (Scope version, resource), in Go, and the result is stored. Row-level security and single-resource `read` checks both read the stored rows; neither evaluates a selector.

**Why.** With one evaluator, opening a resource and listing it can't disagree. Stored rows turn a filtered listing into an index lookup instead of a table scan, and answer "what does this Scope select" and "who can read this resource" without evaluating anything.

## Storage

```sql
scope_generations (scope_id, generation, targets, state)   -- building | active | retired
scope_members     (scope_id, generation, resource_type, resource_id)
scope_pending     (resource_type, resource_id, seq)        -- filled by triggers
```

## Keeping it current

- **Scope edited:** a new generation is built beside the active one, then switched in atomically. Invalid or deleted: every generation is retired at once. _Why:_ an edit never grants a mix of old and new, and revocation doesn't wait for a build.
- **Resource changed:** a trigger queues it, and a worker re-evaluates it every 5 s. Until then the policy hides it. _Why:_ a lag must refuse, never grant (`scopes.md` §7.1).
- **Ordering:** the build, the worker and the switch take a per-type advisory lock. _Why:_ they read and write in separate transactions, and a stale read must not overwrite a newer decision.
- **Whole-type targets** aren't stored. They grant `all`, and drop out of a constrained grant. _Why:_ every resource is a member, so storing them adds rows and no information.

## Claim

Per type: `all`, or grants, each a set of `{scope, generation}` the row must be in all of. A grant is never empty and never repeats a Scope. A grant naming a Scope with no active generation is dropped whole. _Why:_ dropping only that Scope would widen the grant.

## Alternatives

- **Evaluate selectors per row in SQL at read time.** Needs a second evaluator in SQL that can drift from the Go one, caps the selector grammar at what SQL can match, and makes every filtered listing a scan.
- **A `scope_ids` array column on each resource table.** Faster to read, but a Scope edit rewrites rows of the busiest tables while scrapers write to them, and there's no room for a second generation.
- **Per-subject cached resource sets.** Grows with users × resources, and every scrape invalidates it.
- **A relationship store (SpiceDB, OpenFGA).** The right shape, but a second datastore to run, and row-level security already does the lookup once the facts are rows.
- **During a build, grant old and new together, or grant nothing.** The first grants what neither version selects; the second is an outage on every edit.

## Open questions

- Dense grants: is `id IN (members)` or a per-row `EXISTS` faster? Measure.
- How Permissions' inline selectors are enforced on listings.
- Drain interval and batch size: measure at the largest tenant.
