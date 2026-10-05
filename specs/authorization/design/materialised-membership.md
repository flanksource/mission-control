# Materialised Scope Membership

Implements `scopes.md` §4.3 and §7.1, `roles.md` §3.1 and §4.3.

> [!WARNING]
> **This is not the most performant design.** Membership is written in the same transaction that saves a Scope or a resource. That's the simplest thing to build and run: no jobs, queues, retries or recovery after a restart. It's chosen on the assumption that **Scopes are created and changed rarely**.
>
> Measured with 1M configs and 200 Scope targets, each config in about 6–7 Scopes:
>
> - Saving a new config takes about **70 µs** longer. Health and status updates cost nothing.
> - Saving a Scope that selects 500k configs takes **5.4 s**, and **7.3 s** when its targets change. Resources of every type wait to be saved meanwhile.
> - Membership takes about 190 bytes a row: 400 MB for these 1M configs.
>
> If Scopes come to be saved often, or broad Scopes make saving them too slow, move Scope saves to an asynchronous job (Other choices).

## Background

A `read` rule is checked in two places: on one resource when it's opened, and on listings, where row-level security filters the rows (`roles.md` §3.1). Both must allow the same resources. There are two ways to decide whether a resource is in a Scope:

- **Query time.** Every check evaluates the Scope's selectors: in Go for one resource, and in SQL against each row of a listing.
- **Materialised.** Each Scope is evaluated once per resource, when either changes, and the result is stored. Both checks read the stored result.

This design materialises. Query time needs the selector grammar implemented twice, in Go and in SQL, and any difference between the two makes opening and listing disagree. It also caps what a `read` Scope can select at what SQL can match, and makes every filtered listing evaluate every row of the table, so its cost grows with the table rather than the result. Materialising keeps one evaluator, turns the listing filter into an index lookup, and answers "what does this Scope select" and "who can read this resource" without evaluating anything.

Membership is stored synchronously. A resource is matched in the transaction that writes it, and a Scope is built in the transaction that saves it. _Why:_ the Scope language is plain values (`scopes.md` §5), so a target is one row of values, and matching a resource against every target of its type is one predicate over a handful of rows. That's cheap enough to run inside every write, and needs no queue, worker or lag.

Only the membership predicate evaluates a Scope's targets, Views aside (see Not covered). Every check, of every action (`read`, `playbook:run`, `invoke:<plugin>:<operation>`, …), reads the stored result. _Why:_ a second evaluator could disagree with the stored result.

## Storage

```sql
scope_targets (scope_id, resource_type, id, name, name_prefix, namespace, agent_id, types, tags, labels)
scope_members (scope_id, resource_type, resource_id)   -- no resource_id: every resource of the type
```

- **Targets.** A Scope's targets are written as rows when it's saved, with `agent` resolved to an id (`scopes.md` §5.4). A condition a target doesn't set is empty, and matches anything.
- **Whole-type targets** are stored as one `scope_members` row with no `resource_id`, not as a target. _Why:_ every resource of the type is a member, so a row per resource adds rows and no information, and a target with no conditions would otherwise make every write of the type store a row.

## Keeping it current

- **One predicate:** each resource type has one SQL predicate deciding whether a resource matches a target row. It's used both ways: a written resource against every target of its type, and a Scope's targets against every resource. A Scope's rebuild runs it with each target's values as constants, so the tag and label indexes apply. The trigger first finds the targets sharing one `key=value` or namespace with a resource, through an index on `scope_targets`, and runs the predicate on those only. _Why:_ the two can't disagree, and there's no compiler to keep in step with the Scope language beyond writing targets as rows. Measured, constants took a 20k-config rebuild from 3.7 s to 0.2 s, and the keyed lookup made matching negligible next to writing the rows.
- **Resource written:** in the writing transaction, a database trigger replaces the resource's membership rows with the targets it matches. Inserts are matched once per statement, not once per row. Updates are matched only when a field a Scope can select changes (`scopes.md` §5.1), decided before any function runs. A row deleted outright loses its membership; setting `deleted_at` changes nothing. _Why:_ membership then commits with the write. Resources are also written by other processes, such as config-db and canary-checker, so only a trigger sees every write. Health and status change constantly and can't change membership (`scopes.md` §4.3), so they cost nothing.
- **Scope saved:** created or its targets changed, from a CRD or through the UI alike, or re-validated with its `agent` resolving to a different id. Saving it rewrites the Scope's targets and members in the same transaction, and the save returns once that commits. A save whose targets haven't changed rebuilds nothing, and a rebuild writes only the members that change. If the rebuild fails, the save fails and rolls back: the old Scope and its membership stay in force, a CRD's reconcile retries, and the UI gets the error. `Ready` only says whether the Scope is valid; no status reports a rebuild. _Why:_ Postgres shows readers the old rows until the commit and the new ones after, so membership switches at once with no generations to track (`scopes.md` §7.1). A reconcile saves every Scope again on restart, and rewriting a broad Scope's 500k rows each time would take 7 s per Scope. `Ready=False` during a rebuild would stop every Role that references the Scope from applying, deny rules included.
- **Scope invalid or deleted:** its targets and members are deleted in one transaction.
- **Lock:** every write to `scope_targets` takes one advisory lock exclusively, and the trigger takes it shared. _Why:_ a resource written during a rebuild would otherwise be matched against the old targets while the rebuild's snapshot misses it, leaving a row from neither version. One lock for all types, since a rebuild covers every type its Scope selects at once, and locks per type could deadlock against a transaction writing several types. Resource writes wait while a Scope is rebuilt, which takes seconds and only happens when a Scope is saved.
- **Startup:** every valid Scope with neither targets nor members is built before Mission Control accepts requests. _Why:_ a grant through a Scope with no rows is refused, so serving earlier would refuse grants that are in effect.

## Claim

Per type: `all`, for access that names no Scope, or grants, each a set of Scopes the row must be in all of. A grant is never empty and never repeats a Scope. The claim names Scopes only.

A listing admits a row through a Scope when the Scope has a membership row for it, or one with no `resource_id` for the table's type. A Scope with neither admits nothing, so a grant naming it fails whole. _Why:_ a claim then depends only on Roles and RoleBindings, whose changes already refresh it, and stays correct across Scope changes however long it's cached. Dropping only the Scope with no rows would widen the grant.

## Single-resource checks

Every check but a listing is a Casbin check. A Role rule's policy has the shape every policy has, `sub, obj, act, eft, condition, id`, and its Scopes go in `condition`, as a plain Casbin expression over the request.

- **Request.** Before an operation's checks, the memberships of every resource it involves are read in one query, and every check of the operation uses them (`scopes.md` §7.2). They're added to the request beside its other fields, as `Membership`:

  | Field       | Holds                                                                                                                                          |
  | ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
  | `Fits`      | The request has one primary resource of a type the action accepts, at most one target of a type it accepts, and nothing else (`roles.md` §4.3) |
  | `HasTarget` | The request has a target                                                                                                                       |
  | `Resource`  | The Scopes the primary resource is in: those with a membership row for it, and those with one for its whole type                               |
  | `Target`    | The Scopes the target is in, the same way                                                                                                      |

  _Why:_ the condition then only tests whether a value is in a list, which Casbin does natively, so nothing on the Casbin side interprets a rule. Primary and target are kept apart so that a Scope selecting several types can't match through the wrong resource of the request.

- **Condition.** An allow rule's condition holds when the request fits, the primary resource is in every Scope of the rule's resource (its own and the constraint's), and the target is in every Scope of the rule's target, or there's no target when the rule has none. A deny rule's holds when the request doesn't fit, or when the same membership test holds. _Why:_ a request the action doesn't accept never matches an allow and always matches a deny, so it fails closed.
- **Policies name Scopes only**, like the claim. _Why:_ a rebuild, including one that makes a Scope whole-type or not, takes effect on the next check with no policy reload, and a Scope with no rows is in no request's lists, so a grant naming it fails whole.
- **Compatibility.** The policy shape, the matcher and the request's other fields are the same for every policy, so a policy whose condition doesn't read `Membership` evaluates exactly as it would without it. _Why:_ Role rules share one enforcer with every other policy, and must not change how those evaluate.

Example: Alice's binding narrows `read` on `staging-configs` (`s-111`) with a constraint on `tenant-a` (`s-222`). Its policy:

| sub                              | obj | act    | eft     | condition                                                                                                                                          |
| -------------------------------- | --- | ------ | ------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `binding:tenant-a/alice-staging` | `*` | `read` | `allow` | `r.obj.Membership.Fits && !r.obj.Membership.HasTarget && 'scope:s-111' in r.obj.Membership.Resource && 'scope:s-222' in r.obj.Membership.Resource` |

Opening `cfg-X`, a member of both, puts `[scope:s-111, scope:s-222]` in `Resource`, and is allowed. Opening `cfg-Y`, a member of `tenant-a` only, puts `[scope:s-222]`, and is refused. If `staging-configs` were edited to select every config, its rebuild would store one membership row for the whole config type, which puts `scope:s-111` in every config's list, with no change to the policy.

## Permissions

Permissions are kept working only where that costs Role rules nothing (`permissions.md`).

- **Naming Scopes** (`object.scopes`): checked through the Scopes' membership, like a rule, on single resources and on listings.
- **Inline selectors:** not materialised. A single-resource check evaluates them, and a listing grants no rows through them. _Why:_ materialising them needs a second kind of Scope to build and store, for a feature on its way out.
- **Deny on `read`:** not enforced row by row. A subject it applies to lists no rows of any type it covers: an empty result, not `403`. _Why:_ Role rules can't deny `read` (`roles.md` §2), so the claim has no deny, and a deny that can't be enforced must refuse rather than allow (`permissions.md`).

## Not covered

- **Views.** No rule can select Views, and Roles grant no rows of the tables Views generate (`overview.md`, "Not covered yet"). Views keep computing which Scopes each of their rows matches, into their `grants` column, as they do today, and don't read `scope_members`. _Why:_ Views are outside Roles and RoleBindings for now; moving them onto stored membership waits until they're brought in.

## Other choices

- **An asynchronous job for Scope saves.** The way to go if Scopes come to be saved often or broad Scopes make saving them too slow (see the notice): a Scope's membership is built in the background, with its previous membership in force until then. Not taken now because it needs a queue, retries, recovery after restarts, a status to show a build in progress, and lag rules for every check, for Scopes that rarely change.
- **An asynchronous job for resource writes.** Keeps matching out of scraper writes, but needs the same machinery, and a resource's own matching takes microseconds.
- **Generations, built beside the active membership and switched in.** Needed only when a rebuild spans several transactions. In one transaction, Postgres already shows readers the old rows until the commit.
- **Compiling each Scope to its own query.** Two code paths, one per direction, that have to agree. Target rows and one predicate serve both.
- **Re-evaluating on every update.** Health and status writes are most writes, and never change membership.
- **A `scope_ids` array column on each resource table.** Faster to read, but a Scope edit rewrites rows of the busiest tables while scrapers write to them.
- **Per-subject cached resource sets.** Grows with users × resources, and every scrape invalidates it.
- **A relationship store (SpiceDB, OpenFGA).** The right shape, but a second datastore to run, and row-level security already does the lookup once the facts are rows.
- **Scopes in policy columns of their own.** Measured 2.7× faster per check, but only with no `eval` in the matcher: Casbin re-parses a matcher containing `eval` on every check, and the matcher keeps it for the conditions of other policies. With it, columns are no faster than `condition`, and they add columns to every policy.
- **A custom Casbin function over Scope ids.** Decides the same, but the policy no longer says what it requires, and the function has to be kept in step with the request it reads.
- **One membership list per request.** A Scope selecting playbooks and configs would allow a `playbook:run` whose config is in it and whose playbook isn't.

## Open questions

- Repeat the measurements in the notice on the largest real tenant.
- Dense grants: is `id IN (members)` or a per-row `EXISTS` faster? Measure.
- Casbin evaluates every policy on every check, about 1–3 µs each. Measure at the largest tenant's number of bindings.

## FAQ

**Which child tables does row-level security filter, and how?**
The same tables as today, with the same rules: `config_changes`, `config_analysis`, `config_costs`, `config_cost_compact` and `config_component_relationships` show a row when its config is readable; `config_relationships` when both of its configs are; `playbook_runs` when its playbook is, and its config and check when it has them; `checks` when its canary is, or through its own membership. Each of these tests its parent with `EXISTS` on the parent's table, which applies the parent's policy, so only the parents' policies change. `views` and `view_panels` keep today's policies (Not covered).

**Does deleting a resource remove its membership?**
Soft deletion doesn't. `deleted_at` is state, not identity or ownership (`scopes.md` §4.3), so setting it doesn't match the resource again, and audit and history listings keep showing a deleted resource to whoever could read it. A row deleted outright loses its membership in the same transaction.

**What happens if the trigger fails?**
The write fails with it, as with any trigger. That's why the trigger only runs the one predicate, and why it's tested on every resource type.

**What does a deny Permission on `read` do to a listing?**
It empties it: the claim grants no rows of the types the deny covers, so every path that applies the claim enforces it the same way, through PostgREST and through Go alike. It's never `403` (see Permissions).

**Do guests who rely on Permissions with inline selectors lose their listings?**
Yes, and that's accepted (`permissions.md`). Their single-resource checks keep working. It's called out in the release notes. Permissions get no new status condition, since they get no new features.

**What happens to a stored Scope that uses `!=`, `in`, `notin` or a bare key?**
It's `Ready=False` with a reason naming the operator, selects nothing, and the rules that use it stop granting until it's rewritten (`scopes.md` §7). Nothing converts it. _Why:_ an exclusion has no equivalent, rewriting `in` into several targets would change an object its owner manages, e.g. through GitOps, which would revert it, and an authorization object fails closed.

**Does the `X-Flanksource-Scope` header keep working?**
Yes, but it names Scopes instead of carrying selectors. Each Scope it names is added to every grant of the request's claim, so it can only narrow; for a subject whose listings aren't filtered, its Scopes are the claim's only grant. _Why:_ the claim names Scopes only, and a header of selectors would need the per-row evaluator this design removes.

**What if Mission Control runs more than one replica?**
It doesn't: Mission Control runs as a single replica (`AGENTS.md`). The lock is a database lock anyway, so it orders rebuilds against resource writes from every process, config-db and canary-checker included.

**How is a Scope rebuilt when its `agent` name resolves to a different id?**
Every policy reload re-validates every Scope (`scopes.md` §7), and registering or deleting an agent triggers a reload, through the same database notifications as Scopes, Roles and RoleBindings. A Scope whose `agent` resolves to a different id has changed (`scopes.md` §4.3), and is rebuilt in that reload. There's no periodic job.

**Where is `Membership` built, given `Fits` depends on the check?**
Memberships are read once per operation, and `Membership`, `Fits` included, is built from that snapshot for each check, since the primary resource changes between checks: in "run playbook P on config X", P is the primary of `playbook:run` and X of the `read` that follows. Approving a run later is an operation of its own, with its own snapshot.

**How do tests wait for membership?**
They don't need to. A resource's membership commits with its write, and a Scope's with the save that wrote it, through the API or a CRD's reconcile. `Ready` only says whether a Scope is valid, so it's no signal that a rebuild is done.

**Where does the work land?**
In duty first: the tables, the predicate, the triggers, the row-level security policies, the claim, and `Membership` on the request. Then in Mission Control: writing a Scope's targets and rebuilding it when it's saved, compiling conditions, building the claim, and the per-operation snapshot. `matchRule` goes; the Go selector evaluator stays only for Permissions' inline selectors.
