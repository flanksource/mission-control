# Materialised Scope Membership

Implements `scopes.md` §4.3 and §7.1, `roles.md` §3.1.

## Background

A `read` rule is checked in two places: on one resource when it's opened, and on listings, where row-level security filters the rows (`roles.md` §3.1). Both must allow the same resources. There are two ways to decide whether a resource is in a Scope:

- **Query time.** Every check evaluates the Scope's selectors: in Go for one resource, and in SQL against each row of a listing.
- **Materialised.** Each Scope is evaluated once per resource, when either changes, and the result is stored. Both checks read the stored result.

This design materialises. Query time needs the selector grammar implemented twice, in Go and in SQL, and any difference between the two makes opening and listing disagree. It also caps what a `read` Scope can select at what SQL can match, and makes every filtered listing evaluate every row of the table, so its cost grows with the table rather than the result. Materialising keeps one evaluator, turns the listing filter into an index lookup, and answers "what does this Scope select" and "who can read this resource" without evaluating anything.

Only the materializer evaluates selectors. Every check, of every action (`read`, `playbook:run`, `invoke:<plugin>:<operation>`, …), reads the stored result, and a whole-type target is matched by type alone. _Why:_ a second evaluator would disagree with the stored result while it lags.

The price is lag: a change to a Scope or a resource takes effect once it's re-evaluated. `scopes.md` §7.1 bounds what a lag may do: it may refuse, never grant.

## Storage

```sql
scope_generations (scope_id, generation, targets, state)   -- building | active | retired
scope_members     (scope_id, generation, resource_type, resource_id)
scope_pending     (resource_type, resource_id, seq)        -- filled by triggers
```

## Keeping it current

- **Scope edited:** a new generation is built beside the active one, then switched in atomically. Invalid or deleted: every generation is retired at once. _Why:_ an edit never grants a mix of old and new, and revocation doesn't wait for a build.
- **Resource changed:** a trigger queues it, and a worker re-evaluates it every 5 s. Until then it's in no Scope, for every check. _Why:_ a lag must refuse, never grant (`scopes.md` §7.1).
- **Ordering:** the build, the worker and the switch take a per-type advisory lock. _Why:_ they read and write in separate transactions, and a stale read must not overwrite a newer decision.
- **Whole-type targets** aren't stored. They grant `all`, and drop out of a constrained grant. _Why:_ every resource is a member, so storing them adds rows and no information.

## Claim

Per type: `all`, or grants, each a set of `{scope, generation}` the row must be in all of. A grant is never empty and never repeats a Scope. A grant naming a Scope with no active generation is dropped whole. _Why:_ dropping only that Scope would widen the grant.

## Other choices

- **A `scope_ids` array column on each resource table.** Faster to read, but a Scope edit rewrites rows of the busiest tables while scrapers write to them, and there's no room for a second generation.
- **Per-subject cached resource sets.** Grows with users × resources, and every scrape invalidates it.
- **A relationship store (SpiceDB, OpenFGA).** The right shape, but a second datastore to run, and row-level security already does the lookup once the facts are rows.
- **During a build, grant old and new together, or grant nothing.** The first grants what neither version selects; the second is an outage on every edit.

## Open questions

- Dense grants: is `id IN (members)` or a per-row `EXISTS` faster? Measure.
- How Permissions' inline selectors are enforced on listings.
- Drain interval and batch size: measure at the largest tenant.
