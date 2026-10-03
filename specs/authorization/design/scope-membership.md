# Scope Membership

Implements scopes.md Sections [4](../scopes.md#4-membership), [4.3](../scopes.md#43-membership-is-decided-by-the-resource-alone) and [7](../scopes.md#7-how-changes-take-effect), [roles.md Section 3.1](../roles.md#31-the-read-action), [rolebindings.md Section 3.2](../rolebindings.md#32-which-rules-a-constraint-narrows) and [collection-access.md Section 3](../collection-access.md#3-what-each-answer-allows). It changes how a Scope's selectors are evaluated, not what they select.

## 1. Problem

A `read` rule's Scope is evaluated in two places today, by two implementations:

- **One resource.** `matchRule` in duty's `rbac/rule.go` evaluates the Scope's `types.ResourceSelector`s against an `ABACAttribute` in Go.
- **Listings.** `buildRLSPayload` in `auth/rls.go` compiles the same selectors into `rls.Scope` predicates (tags, names, agents, ids), sent to Postgres as the `request.jwt.claims` session setting. The `match_scope()` function in duty's `views/9998_rls_enable.sql` runs them against every candidate row of `config_items`, `components`, `checks`, `canaries` and `playbooks`.

Three costs follow:

1. `match_scope()` is a plpgsql function over jsonb, so no index helps. Every filtered listing scans the table, and cost grows with table size, not result size.
2. The selector grammar a `read` rule may use is capped at what `match_scope()` can evaluate: the `rowFilterFields` allowlist in `rbac/adapter/row_filter.go`, and the field table in [roles.md Section 3.1](../roles.md#31-the-read-action). Anything richer, which `ResourceSelector.Matches` already supports, is rejected to keep the two implementations in agreement.
3. Nothing stored says which resources a Scope selects, so there is no cheap answer to "does this subject's access cover any config", no way to preview a Scope before saving it, and no way to list who can read a resource (`rbac/access_review.go` re-evaluates).

Views already avoid all three: `views/scopes.go` evaluates every Scope against each row at refresh time and stores the matching scope ids in the view table's `grants` column, and `views/035_view_rls.sql` checks that column against the `scopes` claim. This design generalises that approach to the resource tables.

## 2. Design

Selectors remain the authoring language; **membership rows** become the enforcement representation.

- A Scope's non-whole-type targets are evaluated in Go, with `ResourceSelector.Matches`, once per (Scope, row) whenever either changes. The result is stored in `scope_members`.
- Row-level security, single-resource checks and the collection-level derivation all consult `scope_members`. None of them evaluates a selector.
- Whole-type targets ([scopes.md Section 5.2](../scopes.md#52-wildcards-and-patterns)) are never materialised. They compile to the unconditional object policy as today (`wholeTypeObjects` in `rbac/adapter/role.go`) and set the type to `all` in the claim.

## 3. Storage

```sql
CREATE TABLE scope_members (
  scope_id      uuid NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
  resource_type text NOT NULL,            -- config | component | check | canary | playbook
  resource_id   uuid NOT NULL,
  generation    bigint NOT NULL,          -- the Scope reconcile that produced the row
  PRIMARY KEY (resource_type, resource_id, scope_id)
);
CREATE INDEX scope_members_scope_idx ON scope_members (scope_id, resource_type);
```

- The primary key serves the row-level security predicate (row in hand, is it in any of these scopes?). The secondary index serves the scope-side reconcile and the collection-level existence check.
- Connections have no membership: they can only be granted whole (`row_filter.go`), which stays an object policy.
- Views keep their `grants` column. Converging them onto `scope_members` is a later change.
- `resource_id` has no foreign key, since the five tables differ. Membership of a soft-deleted resource is removed by the row-side reconcile ([Section 5.2](#52-row-side)).

### 3.1 Why a side table and not a column

|                              | `scope_members` table          | `scope_ids uuid[]` column on each table                                                  |
| ---------------------------- | ------------------------------ | ---------------------------------------------------------------------------------------- |
| Row-level security predicate | Indexed `EXISTS`               | `&&` with a GIN index, no join                                                           |
| Row changes                  | Delete and insert for that row | Rewrite the array in the same update                                                     |
| Scope edit                   | Touches that Scope's rows only | Updates every row whose membership changes, on `config_items` while scrapers write to it |
| New protected type           | Nothing                        | A column and an index per table                                                          |

The column is faster to read. The table keeps an admin saving a Scope from contending with scrapers on the busiest table, and keeps one Scope's recompute independent of the others. The read cost of the join is bounded by the number of rows the query already touches. Start with the table; add the column as a cache for one table if the join shows in profiles.

## 4. Evaluation

Membership is computed by one function, `MembersOf(scope ScopeSelection, row types.ResourceSelectable) bool`, which applies [scopes.md Section 4](../scopes.md#4-membership): OR across the Scope's targets of the row's type, AND within a target, through `ResourceSelector.Matches`. `models.ConfigItem`, `Component`, `Check`, `Canary` and `Playbook` already implement `ResourceSelectable`.

The same function is used by both reconcile paths and nowhere else. `matchRule`'s selector evaluation and `match_scope()` stop being used for Scopes ([Section 6](#6-enforcement)).

Every field in [scopes.md Section 5.1](../scopes.md#51-fields) is a function of the row alone, so every current Scope can be materialised. The `rowFilterFields` allowlist, and the field table in [roles.md Section 3.1](../roles.md#31-the-read-action), are deleted. The constraint that replaces them is [scopes.md Section 4.3](../scopes.md#43-membership-is-decided-by-the-resource-alone): a selector may not depend on another row. Nothing in the current grammar does; a future relationship-based selector would have to be rejected by `ValidateScope`.

## 5. Reconciliation

Two paths keep `scope_members` current. Both are asynchronous; [Section 5.3](#53-windows) states the windows.

### 5.1 Scope-side

Triggered by `PersistScope`, `DeleteScope`, `DeleteStaleScope` in `db/scope.go`, and by the periodic re-validation of [scopes.md Section 7](../scopes.md#7-how-changes-take-effect) when an `agent` resolves differently.

For each type the Scope declares with a non-whole-type target: stream the table in batches of 5,000 by primary key, evaluate each row, and insert matches with the new `generation`. When the scan completes, delete the Scope's rows with an older generation. A Scope that becomes invalid, or is deleted, has all its rows deleted (an invalid Scope selects nothing, [scopes.md Section 7](../scopes.md#7-how-changes-take-effect); the foreign key handles hard deletes).

Using a generation means the Scope's previous members stay granted until the new set is complete, so a subject never sees an empty listing in the middle of a recompute.

The reconcile runs as a `duty/job` singleton, one Scope at a time, never in a request path. It records on the Scope: `status.conditions[MembersReconciled]` with `generation`, `lastTransitionTime` and the member count per type. The API returns the same. This is the "stated window" of [scopes.md Section 7](../scopes.md#7-how-changes-take-effect).

### 5.2 Row-side

Rows are written by other processes: `config_items` by config-db, `checks` and `canaries` by canary-checker, `components` by topology scrapers. Mission Control can't hook those writes, so the database signals them:

```sql
CREATE TABLE scope_members_pending (
  resource_type text NOT NULL,
  resource_id   uuid NOT NULL,
  queued_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (resource_type, resource_id)
);
```

A trigger on each of the five tables inserts into `scope_members_pending` (`ON CONFLICT DO NOTHING`) after an insert, after a delete, and after an update that changes a column a selector can read: `name`, `namespace`, `tags`, `labels`, `agent_id`, `type`, `status`, `health`, `deleted_at`, and the remaining fields `AsMap()` exposes. Updates that touch none of them, which is most of config-db's traffic (`config`, `last_scraped_time`, timestamps), fire nothing.

A worker drains the pending table every 5 seconds: it takes up to 10,000 entries, loads the rows, evaluates each against every valid Scope declaring its type (the Scope list is cached, as `views/scopes.go` caches it, and flushed when a Scope changes), replaces the rows' membership in one transaction, and deletes the drained entries. A deleted or soft-deleted row ends with no membership.

### 5.3 Windows

| Change                            | Visible to row-filtered subjects within                                                    | Transient                                                                                |
| --------------------------------- | ------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------- |
| Resource created or changed       | ~5 s, one drain cycle                                                                      | A resource that left a Scope stays readable until drained; one that entered it isn't yet |
| Scope created, changed or deleted | The reconcile's duration: seconds for small tables, minutes for a full `config_items` scan | The previous members stay granted until the new set is complete                          |
| Binding or Role changed           | Immediate; the claim changes, membership doesn't                                           | None                                                                                     |

The over-grant transient on the row side is the price of asynchronous maintenance. It is bounded by the drain interval and applies only to a resource whose selectable fields changed in a way that removed it from a Scope. If a deployment needs it tighter, the drain interval is a property; it can't be zero without evaluating selectors inside the writers' transactions, which this design rules out.

### 5.4 Safety net

A job recomputes every valid Scope's membership every 6 hours into a shadow generation, counts rows that differ from the live set, publishes the count as the metric `scope_members_drift`, and promotes the shadow set. Any non-zero drift is a bug in a reconcile path and alerts.

## 6. Enforcement

### 6.1 Claim

`rls.Payload` carries, per resource type, either `all: true` or a list of grants, each a set of scope ids the row must belong to all of:

```json
{
  "config": {
    "grants": [
      { "scopes": ["<media-configs>"] },
      { "scopes": ["<prod>", "<tenant-a>"] }
    ]
  },
  "playbook": { "all": true }
}
```

- A `read` rule without a constraint is one grant with one scope. A rule narrowed by a constraint ([rolebindings.md Section 3.2](../rolebindings.md#32-which-rules-a-constraint-narrows)) is one grant with two: the rule's Scope and the constraint's. The row must be in both, which is the intersection the specification requires.
- A whole-type grant on a type sets `all`. The type's object policy already passed the collection-level check.
- A type with neither is absent from the claim: `none`.

`buildRLSPayload` produces this from `adapter.LoadBindingRules`, which already resolves each rule's `Resource` to its `ScopeSelection`s. Legacy Permissions with inline selectors (`addPermissionFilters`) are compiled into hidden Scopes keyed by Permission id, so they flow through the same path; this keeps `match_scope()` out of the predicate entirely rather than running two predicates for the transition.

### 6.2 Row-level security policy

For `config_items`, and likewise for the other four tables:

```sql
USING (
  is_rls_disabled()
  OR (claims -> 'config' ->> 'all')::boolean
  OR EXISTS (
    SELECT 1
    FROM jsonb_array_elements(claims -> 'config' -> 'grants') g
    WHERE (
      SELECT count(DISTINCT m.scope_id)
      FROM scope_members m
      WHERE m.resource_type = 'config'
        AND m.resource_id = config_items.id
        AND m.scope_id = ANY (ARRAY(SELECT jsonb_array_elements_text(g -> 'scopes'))::uuid[])
    ) = jsonb_array_length(g -> 'scopes')
  )
)
```

Each `EXISTS` is a primary-key lookup on `scope_members`. `match_scope()` is no longer referenced by these five policies.

### 6.3 Single-resource checks

`HasPermission` for `read` consults `scope_members` for the resource's scope ids and matches them against the subject's grants, instead of evaluating selectors. Opening a resource and listing it then read the same stored fact, which is what [roles.md Section 3.1](../roles.md#31-the-read-action) requires, and both are subject to the same windows in [Section 5.3](#53-windows). Actions other than `read` keep evaluating selectors in `matchRule`: they're checked per request, against resources already loaded, and aren't filtered by row.

### 6.4 Collection-level

[collection-access.md](../collection-access.md) derives all, some or none from grants, so it needs nothing from `scope_members`. Membership adds what the specification leaves to the design:

- `GET /scopes/{id}/members?type=config&limit=` for the Scope's admin page, and a dry-run of the same on a Scope being edited, evaluated in Go over a sample without storing.
- "Who can read this resource" in `rbac/access_review.go`: scope ids from `scope_members`, then the bindings whose rules name them.

## 7. Cost

Per resource change: one evaluation per valid Scope declaring its type. `ResourceSelector.Matches` on a tag selector is microseconds; a `fieldSelector` or `labelSelector` is comparable. With 200 Scopes and 10,000 changed rows in a drain, that's 2 million evaluations, roughly 2 to 5 seconds of one core, in a batch job off the request path.

Per Scope change: one scan of each declared table. A million-row `config_items` streams in batches of 5,000 in low minutes.

Per read: unchanged query shape plus a primary-key lookup per candidate row. Measured against the current `match_scope()` scan, the listing cost becomes proportional to the page size rather than the table size.

**To measure before accepting:** drain latency and CPU on the largest tenant's Scope count and change rate; `config_items` scan time per Scope change at that tenant; p95 of the Catalog listing for a guest who may read some configs, before and after. Budgets: drain keeps up at twice the observed peak change rate; a Scope change on the largest tenant reconciles in under 10 minutes; the filtered listing is no slower than the unfiltered one.

## 8. Alternatives

- **Keep evaluating per row, and extend `match_scope()`.** Keeps two implementations in step by hand, and every listing stays a scan. This is what the field tables in [roles.md](../roles.md) exist to contain.
- **Array column per table** (as views do). Rejected for the resource tables for the contention reason in [Section 3.1](#31-why-a-side-table-and-not-a-column); kept for views, which are rebuilt wholesale.
- **Compute each subject's member set at request time and cache it.** Per-user caches of resource ids don't scale with users times resources, and invalidation on every scrape defeats the cache.
- **A relationship store such as SpiceDB or OpenFGA.** The right shape, but a second datastore to run, and Postgres row-level security already does the lookup once the facts are rows.
- **Evaluate selectors in SQL triggers.** Puts the selector grammar back into plpgsql, which is the duplication being removed.

## 9. Rollout

1. Migration: `scope_members`, `scope_members_pending`, the five triggers, the Scope status fields. No behaviour change.
2. Backfill: the [Section 5.4](#54-safety-net) job runs once for every Scope.
3. Shadow: with `rls.membership.shadow=true`, listings keep the current predicate and the worker logs rows where the membership predicate would differ. Run for a release.
4. Switch: `rls.membership.enabled=true` installs the new policies and claim; `buildRLSPayload` emits scope ids. The `rowFilterFields` allowlist is removed in the same release, which widens what a `read` rule's Scope may use to everything [scopes.md Section 5](../scopes.md#5-selector-language) allows.
5. Later: views converge; legacy Permission selectors are retired with Permissions.

Nothing in the specifications changes meaning across these steps; only the transient windows of [Section 5.3](#53-windows) appear.

## 10. Open questions

- **Drain interval and batch size** are guesses until measured ([Section 7](#7-cost)).
- **Hidden Scopes for Permissions** ([Section 6.1](#61-claim)): whether to compile them, or keep `match_scope()` for Permission filters alone until Permissions are retired. Compiling is one mechanism; keeping is less migration.
- **`health` and `status` in Scopes.** Row-local, so allowed, but a Scope on `health=unhealthy` churns membership on every health flip. Whether to allow them in `read` rules at all is a specification question for [roles.md](../roles.md).
