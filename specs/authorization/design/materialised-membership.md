# Materialised Scope Membership

Implements `scopes.md` §4.3 and §7.1, `roles.md` §3.1 and §4.3.

## Background

A `read` rule is checked in two places: on one resource when it's opened, and on listings, where row-level security filters the rows (`roles.md` §3.1). Both must allow the same resources. There are two ways to decide whether a resource is in a Scope:

- **Query time.** Every check evaluates the Scope's selectors: in Go for one resource, and in SQL against each row of a listing.
- **Materialised.** Each Scope is evaluated once per resource, when either changes, and the result is stored. Both checks read the stored result.

This design materialises. Query time needs the selector grammar implemented twice, in Go and in SQL, and any difference between the two makes opening and listing disagree. It also caps what a `read` Scope can select at what SQL can match, and makes every filtered listing evaluate every row of the table, so its cost grows with the table rather than the result. Materialising keeps one evaluator, turns the listing filter into an index lookup, and answers "what does this Scope select" and "who can read this resource" without evaluating anything.

Only the materializer evaluates a Scope's selectors. Every check, of every action (`read`, `playbook:run`, `invoke:<plugin>:<operation>`, …), reads the stored result, and a whole-type target is matched by type alone. _Why:_ a second evaluator would disagree with the stored result while it lags.

The price is lag: a change to a Scope or a resource takes effect once it's re-evaluated. `scopes.md` §7.1 bounds what a lag may do: keep the old state or refuse, never grant what neither state grants.

## Storage

```sql
scope_generations (scope_id, generation, targets, state)   -- building | active | retired
scope_members     (scope_id, generation, resource_type, resource_id)
scope_pending     (resource_type, resource_id, seq)        -- filled by triggers
```

## Keeping it current

- **Scope edited:** a new generation is built beside the active one, then switched in atomically. Invalid or deleted: every generation is retired at once. _Why:_ an edit never grants a mix of old and new, and revocation doesn't wait for a build.
- **Resource changed:** a trigger queues it when a field a Scope can select changes (`scopes.md` §5.1), and a worker re-evaluates it every 5 s, replacing all its rows in one transaction. Until then it keeps its previous membership; a new resource has none. _Why:_ the previous membership is the old state, which a lag may keep (`scopes.md` §7.1), so a change costs the scraper's write nothing but the queue entry. Other fields can't change membership (`scopes.md` §4.3), so queueing them is wasted work.
- **Ordering:** the build, the worker and the switch take a per-type advisory lock. _Why:_ they read and write in separate transactions, and a stale read must not overwrite a newer decision.
- **Whole-type targets** aren't stored. They grant `all`, and drop out of a constrained grant. _Why:_ every resource is a member, so storing them adds rows and no information.
- **Startup:** every valid Scope without an active generation is built before Mission Control accepts requests. _Why:_ a grant through a Scope with no active generation is refused, so serving earlier would refuse grants that are in effect.

## Claim

Per type: `all`, or grants, each a set of `{scope, generation}` the row must be in all of. A grant is never empty and never repeats a Scope. A grant naming a Scope with no active generation is dropped whole. _Why:_ dropping only that Scope would widen the grant.

## Single-resource checks

Every check but a listing is a Casbin check. A Role rule's policy has the shape every policy has, `sub, obj, act, eft, condition, id`, and its Scopes go in `condition`, as a plain Casbin expression over the request.

- **Request.** Before a check, the memberships of the resources it names are read from the active generations in one query, and added to the request beside its other fields, as `Membership`:

  | Field       | Holds                                                                                                                                          |
  | ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
  | `Fits`      | The request has one primary resource of a type the action accepts, at most one target of a type it accepts, and nothing else (`roles.md` §4.3) |
  | `HasTarget` | The request has a target                                                                                                                       |
  | `Resource`  | The primary resource's Scopes, and its type                                                                                                    |
  | `Target`    | The target's Scopes, and its type                                                                                                              |

  _Why:_ the condition then only tests whether a value is in a list, which Casbin does natively, so nothing on the Casbin side interprets a rule. Primary and target are kept apart so that a Scope selecting several types can't match through the wrong resource of the request.

- **Condition.** An allow rule's condition holds when the request fits, the primary resource is in every Scope of the rule's resource (its own and the constraint's), and the target is in every Scope of the rule's target, or there's no target when the rule has none. A deny rule's holds when the request doesn't fit, or when the same membership test holds. A Scope also matches the type of each of its whole-type targets. _Why:_ a request the action doesn't accept never matches an allow and always matches a deny, so it fails closed.
- **Policies name Scopes, not generations.** _Why:_ a switch takes effect on the next check with no policy reload, and a Scope with no active generation has no members, so a grant naming it fails whole, as in the claim.
- **Compatibility.** The policy shape, the matcher and the request's other fields are the same for every policy, so a policy whose condition doesn't read `Membership` evaluates exactly as it would without it. _Why:_ Role rules share one enforcer with every other policy, and must not change how those evaluate.

Example: Alice's binding narrows `read` on `staging-configs` (`s-111`) with a constraint on `tenant-a` (`s-222`). Its policy:

| sub                              | obj | act    | eft     | condition                                                                                                                                          |
| -------------------------------- | --- | ------ | ------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `binding:tenant-a/alice-staging` | `*` | `read` | `allow` | `r.obj.Membership.Fits && !r.obj.Membership.HasTarget && 'scope:s-111' in r.obj.Membership.Resource && 'scope:s-222' in r.obj.Membership.Resource` |

Opening `cfg-X`, a member of both, puts `[type:config, scope:s-111, scope:s-222]` in `Resource`, and is allowed. Opening `cfg-Y`, a member of `tenant-a` only, puts `[type:config, scope:s-222]`, and is refused.

## Permissions

Permissions are kept working only where that costs Role rules nothing (`permissions.md`).

- **Naming Scopes** (`object.scopes`): checked through the Scopes' membership, like a rule, on single resources and on listings.
- **Inline selectors:** not materialised. A single-resource check evaluates them, and a listing grants no rows through them. _Why:_ materialising them needs a second kind of Scope to build and store, for a feature on its way out.
- **Deny on `read`:** not enforced on listings. A subject it applies to is refused the listing of every type it covers. _Why:_ Role rules can't deny `read` (`roles.md` §2), so the claim has no deny, and an unenforced deny must refuse rather than allow (`permissions.md`).

## Other choices

- **A `scope_ids` array column on each resource table.** Faster to read, but a Scope edit rewrites rows of the busiest tables while scrapers write to them, and there's no room for a second generation.
- **Per-subject cached resource sets.** Grows with users × resources, and every scrape invalidates it.
- **A relationship store (SpiceDB, OpenFGA).** The right shape, but a second datastore to run, and row-level security already does the lookup once the facts are rows.
- **During a build, grant old and new together, or grant nothing.** The first grants what neither version selects; the second is an outage on every edit.
- **Scopes in policy columns of their own.** Measured 2.7× faster per check, but only with no `eval` in the matcher: Casbin re-parses a matcher containing `eval` on every check, and the matcher keeps it for the conditions of other policies. With it, columns are no faster than `condition`, and they add columns to every policy.
- **A custom Casbin function over Scope ids.** Decides the same, but the policy no longer says what it requires, and the function has to be kept in step with the request it reads.
- **One membership list per request.** A Scope selecting playbooks and configs would allow a `playbook:run` whose config is in it and whose playbook isn't.

## Open questions

- Dense grants: is `id IN (members)` or a per-row `EXISTS` faster? Measure.
- Drain interval and batch size: measure at the largest tenant.
- Casbin evaluates every policy on every check, about 1–3 µs each. Measure at the largest tenant's number of bindings.
- Startup build time with every Scope unbuilt: measure at the largest tenant.
