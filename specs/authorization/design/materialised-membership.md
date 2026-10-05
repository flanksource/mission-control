# Materialised Scope Membership

Implements `scopes.md` §4.3 and §7.1, `roles.md` §3.1 and §4.3.

## Background

A `read` rule is checked in two places: on one resource when it's opened, and on listings, where row-level security filters the rows (`roles.md` §3.1). Both must allow the same resources. There are two ways to decide whether a resource is in a Scope:

- **Query time.** Every check evaluates the Scope's selectors: in Go for one resource, and in SQL against each row of a listing.
- **Materialised.** Each Scope is evaluated once per resource, when either changes, and the result is stored. Both checks read the stored result.

This design materialises. Query time needs the selector grammar implemented twice, in Go and in SQL, and any difference between the two makes opening and listing disagree. It also caps what a `read` Scope can select at what SQL can match, and makes every filtered listing evaluate every row of the table, so its cost grows with the table rather than the result. Materialising keeps one evaluator, turns the listing filter into an index lookup, and answers "what does this Scope select" and "who can read this resource" without evaluating anything.

Only the materializer evaluates a Scope's selectors, Views aside (see Not covered). Every check, of every action (`read`, `playbook:run`, `invoke:<plugin>:<operation>`, …), reads the stored result, and a whole-type target is matched by type alone. _Why:_ a second evaluator would disagree with the stored result while it lags.

The price is lag: a change to a Scope or a resource takes effect once it's re-evaluated. `scopes.md` §7.1 bounds what a lag may do: keep the old state or refuse, never grant what neither state grants.

## Storage

```sql
scope_generations (scope_id, generation, targets, state)   -- building | active | retired
scope_members     (scope_id, generation, resource_type, resource_id)
scope_pending     (resource_type, resource_id, seq)        -- filled by triggers
```

## Keeping it current

- **One evaluator:** selectors are compiled to SQL by one compiler for the Scope language (`scopes.md` §5), and nothing else evaluates them. A Scope edit runs a Scope's query over its tables; a resource's re-evaluation runs the same queries restricted to the queued ids. _Why:_ the two can't disagree about a resource, a build stays in the database, and duty's general query builder differs from the Scope language (it folds case and treats `%` and `_` as wildcards), so it isn't reused.
- **Scope edited:** a new generation is built beside the active one, then switched in atomically. Invalid or deleted: every generation is retired at once. _Why:_ an edit never grants a mix of old and new, and revocation doesn't wait for a build.
- **Resource changed:** a trigger queues it when a field a Scope can select changes (`scopes.md` §5.1), and a worker re-evaluates it every 5 s, replacing all its rows in one transaction. Until then it keeps its previous membership; a new resource has none. _Why:_ the previous membership is the old state, which a lag may keep (`scopes.md` §7.1), so a change costs the scraper's write nothing but the queue entry. Other fields can't change membership (`scopes.md` §4.3), so queueing them is wasted work.
- **Pending queue:** one entry per resource, whose `seq` rises on every change. The worker reads entries together with the resources' current fields in one query, and in the transaction that replaces a resource's rows deletes its entry only if its `seq` is unchanged. _Why:_ a change queued after the read raises `seq`, so its entry survives and is evaluated on the next pass; reading both in one query means an acknowledged `seq` is never newer than the state evaluated.
- **Ordering:** the build, the worker and the switch take a per-type advisory lock. _Why:_ they read and write in separate transactions, and a stale read must not overwrite a newer decision.
- **Whole-type targets** aren't stored. A Scope whose active generation has a whole-type target of a type admits every resource of that type, decided each time a check or listing runs. _Why:_ every resource is a member, so storing them adds rows and no information, and deciding it when it's used follows a switch with nothing to invalidate.
- **Startup:** every valid Scope without an active generation is built before Mission Control accepts requests. _Why:_ a grant through a Scope with no active generation is refused, so serving earlier would refuse grants that are in effect.

## Claim

Per type: `all`, for access that names no Scope, or grants, each a set of Scopes the row must be in all of. A grant is never empty and never repeats a Scope. The claim names Scopes only: no generations, and no Scope is left out of a grant for being whole-type.

A listing resolves each Scope's active generation when it runs. A Scope whose active generation has a whole-type target of the table's type admits every row; otherwise its members do. A Scope with no active generation admits none, so a grant naming it fails whole.

_Why:_ what depends on a generation is read where the generation is, so a claim depends only on Roles and RoleBindings, whose changes already refresh it, and stays correct however long it's cached. Dropping only a Scope with no active generation would widen the grant.

## Single-resource checks

Every check but a listing is a Casbin check. A Role rule's policy has the shape every policy has, `sub, obj, act, eft, condition, id`, and its Scopes go in `condition`, as a plain Casbin expression over the request.

- **Request.** Before an operation's checks, the memberships of every resource it involves are read from the active generations in one query, and every check of the operation uses them (`scopes.md` §7.2). They're added to the request beside its other fields, as `Membership`:

  | Field       | Holds                                                                                                                                          |
  | ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
  | `Fits`      | The request has one primary resource of a type the action accepts, at most one target of a type it accepts, and nothing else (`roles.md` §4.3) |
  | `HasTarget` | The request has a target                                                                                                                       |
  | `Resource`  | The Scopes the primary resource is in: those it's a member of, and those whose active generation has a whole-type target of its type           |
  | `Target`    | The Scopes the target is in, the same way                                                                                                      |

  _Why:_ the condition then only tests whether a value is in a list, which Casbin does natively, so nothing on the Casbin side interprets a rule. Primary and target are kept apart so that a Scope selecting several types can't match through the wrong resource of the request.

- **Condition.** An allow rule's condition holds when the request fits, the primary resource is in every Scope of the rule's resource (its own and the constraint's), and the target is in every Scope of the rule's target, or there's no target when the rule has none. A deny rule's holds when the request doesn't fit, or when the same membership test holds. _Why:_ a request the action doesn't accept never matches an allow and always matches a deny, so it fails closed.
- **Policies name Scopes only**, like the claim. _Why:_ a switch, including one that makes a Scope whole-type or not, takes effect on the next check with no policy reload, and a Scope with no active generation is in no request's lists, so a grant naming it fails whole.
- **Compatibility.** The policy shape, the matcher and the request's other fields are the same for every policy, so a policy whose condition doesn't read `Membership` evaluates exactly as it would without it. _Why:_ Role rules share one enforcer with every other policy, and must not change how those evaluate.

Example: Alice's binding narrows `read` on `staging-configs` (`s-111`) with a constraint on `tenant-a` (`s-222`). Its policy:

| sub                              | obj | act    | eft     | condition                                                                                                                                          |
| -------------------------------- | --- | ------ | ------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `binding:tenant-a/alice-staging` | `*` | `read` | `allow` | `r.obj.Membership.Fits && !r.obj.Membership.HasTarget && 'scope:s-111' in r.obj.Membership.Resource && 'scope:s-222' in r.obj.Membership.Resource` |

Opening `cfg-X`, a member of both, puts `[scope:s-111, scope:s-222]` in `Resource`, and is allowed. Opening `cfg-Y`, a member of `tenant-a` only, puts `[scope:s-222]`, and is refused. If `staging-configs` were edited to select every config, its next active generation would put `scope:s-111` in every config's list, with no change to the policy.

## Permissions

Permissions are kept working only where that costs Role rules nothing (`permissions.md`).

- **Naming Scopes** (`object.scopes`): checked through the Scopes' membership, like a rule, on single resources and on listings.
- **Inline selectors:** not materialised. A single-resource check evaluates them, and a listing grants no rows through them. _Why:_ materialising them needs a second kind of Scope to build and store, for a feature on its way out.
- **Deny on `read`:** not enforced row by row. A subject it applies to lists no rows of any type it covers: an empty result, not `403`. _Why:_ Role rules can't deny `read` (`roles.md` §2), so the claim has no deny, and a deny that can't be enforced must refuse rather than allow (`permissions.md`).

## Not covered

- **Views.** No rule can select Views, and Roles grant no rows of the tables Views generate (`overview.md`, "Not covered yet"). Views keep computing which Scopes each of their rows matches, into their `grants` column, as they do today, and don't read `scope_members`. _Why:_ Views are outside Roles and RoleBindings for now; moving them onto stored membership waits until they're brought in.

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

## FAQ

**Which child tables does row-level security filter, and how?**
The same tables as today, with the same rules: `config_changes`, `config_analysis`, `config_costs`, `config_cost_compact` and `config_component_relationships` show a row when its config is readable; `config_relationships` when both of its configs are; `playbook_runs` when its playbook is, and its config and check when it has them; `checks` when its canary is, or through its own membership. Each of these tests its parent with `EXISTS` on the parent's table, which applies the parent's policy, so only the parents' policies change. `views` and `view_panels` keep today's policies (Not covered).

**Does deleting a resource remove its membership?**
Soft deletion doesn't. `deleted_at` is state, not identity or ownership (`scopes.md` §4.3), so setting it isn't queued, and audit and history listings keep showing a deleted resource to whoever could read it. A resource whose row is deleted outright is queued like any change, and the worker, finding no row, removes its membership.

**What does a deny Permission on `read` do to a listing?**
It empties it: the claim grants no rows of the types the deny covers, so every path that applies the claim enforces it the same way, through PostgREST and through Go alike. It's never `403` (see Permissions).

**Do guests who rely on Permissions with inline selectors lose their listings?**
Yes, and that's accepted (`permissions.md`). Their single-resource checks keep working. It's called out in the release notes. Permissions get no new status condition, since they get no new features.

**What happens to a stored Scope that uses `!=`, `in`, `notin` or a bare key?**
It's `Ready=False` with a reason naming the operator, selects nothing, and the rules that use it stop granting until it's rewritten (`scopes.md` §7). Nothing converts it. _Why:_ an exclusion has no equivalent, rewriting `in` into several targets would change an object its owner manages, e.g. through GitOps, which would revert it, and an authorization object fails closed.

**Does the `X-Flanksource-Scope` header keep working?**
Yes, but it names Scopes instead of carrying selectors. Each Scope it names is added to every grant of the request's claim, so it can only narrow; for a subject whose listings aren't filtered, its Scopes are the claim's only grant. _Why:_ the claim names Scopes only, and a header of selectors would need the per-row evaluator this design removes.

**What if Mission Control runs more than one replica?**
It doesn't: Mission Control runs as a single replica (`AGENTS.md`). The worker, builds and switches run in that one process, and the per-type advisory lock only orders them within it.

**How is a Scope rebuilt when its `agent` name resolves to a different id?**
A periodic job re-validates every Scope (`scopes.md` §7), and registering or deleting an agent triggers it too. A Scope whose `agent` resolves to a different id has changed, and gets a new generation (`scopes.md` §4.3). An invalid Scope takes effect "immediately" (`scopes.md` §7.1) from when re-validation marks it invalid.

**When are a retired generation's rows deleted?**
Right after the switch commits, in batches. _Why:_ claims and policies never name a generation, and a statement already running keeps its snapshot, so nothing reads them once the switch is visible.

**Where is `Membership` built, given `Fits` depends on the check?**
Memberships are read once per operation, and `Membership`, `Fits` included, is built from that snapshot for each check, since the primary resource changes between checks: in "run playbook P on config X", P is the primary of `playbook:run` and X of the `read` that follows. Approving a run later is an operation of its own, with its own snapshot.

**How do tests avoid waiting for the worker?**
A test hook runs the worker until the queue is empty. Tests never sleep through the drain interval.

**Where does the work land?**
In duty first: the tables, triggers, the Scope-to-SQL compiler, the row-level security policies, the claim, and `Membership` on the request. Then in Mission Control: the worker and builds, compiling conditions, building the claim, and the per-operation snapshot. `matchRule` goes; the Go selector evaluator stays only for Permissions' inline selectors.
