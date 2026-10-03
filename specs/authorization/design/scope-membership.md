# Scope Membership

Implements scopes.md Sections [4](../scopes.md#4-membership), [4.3](../scopes.md#43-membership-is-decided-by-the-resource-alone) and [7.1](../scopes.md#71-membership-changes), [roles.md Section 3.1](../roles.md#31-the-read-action) and [rolebindings.md Section 3.2](../rolebindings.md#32-which-rules-a-constraint-narrows). It changes how a Scope's selectors are evaluated, not what they select. Where it mentions collection-level access, it refers to `collection-access.md` (flanksource/mission-control#3515).

## 1. Problem

A `read` rule's Scope is evaluated in two places today, by two implementations:

- **One resource.** `matchRule` in duty's `rbac/rule.go` evaluates the Scope's `types.ResourceSelector`s against an `ABACAttribute` in Go.
- **Listings.** `buildRLSPayload` in `auth/rls.go` compiles the same selectors into `rls.Scope` predicates (tags, names, agents, ids), sent to Postgres as the `request.jwt.claims` session setting. The `match_scope()` function in duty's `views/9998_rls_enable.sql` runs them against every candidate row of `config_items`, `components`, `checks`, `canaries` and `playbooks`.

Three costs follow:

1. `match_scope()` is a plpgsql function over jsonb, so no index helps. Every filtered listing scans the table, and cost grows with table size, not result size.
2. The selector grammar a `read` rule may use is capped at what `match_scope()` can evaluate: the `rowFilterFields` allowlist in `rbac/adapter/row_filter.go`, and the field table formerly in roles.md Section 3.1. Anything richer, which `ResourceSelector.Matches` already supports, is rejected to keep the two implementations in agreement.
3. Nothing stored says which resources a Scope selects, so there is no cheap answer to "does this subject's access cover any config", no way to preview a Scope before saving it, and no way to list who can read a resource (`rbac/access_review.go` re-evaluates).

Views already avoid all three: `views/scopes.go` evaluates every Scope against each row at refresh time and stores the matching scope ids in the view table's `grants` column, and `views/035_view_rls.sql` checks that column against the `scopes` claim. This design generalises that approach to the resource tables.

## 2. Design

Selectors remain the authoring language; **membership rows** become the enforcement representation for `read`.

- A Scope's targets are evaluated in Go, with `ResourceSelector.Matches`, once per (Scope version, row) whenever either changes. The result is stored in `scope_members`, tagged with the Scope version (its **generation**) that produced it.
- Row-level security and single-resource `read` checks consult `scope_members` for the generation currently in effect. Neither evaluates a selector.
- A Scope that is **whole-type for a type** has a whole-type target of it ([scopes.md Section 5.2](../scopes.md#52-wildcards-and-patterns)), whatever its other targets of that type, since targets combine with OR. It's never materialised for that type. It compiles to the unconditional object policy as today (`wholeTypeObjects` in `rbac/adapter/role.go`) and, in the claim, contributes `all` or drops out of a conjunction ([Section 6.1](#61-claim)).
- A resource whose membership is waiting to be re-evaluated is in no materialised Scope until it is ([Section 5.5](#55-windows)). The delay can hide a resource; it can't expose one.

## 3. Storage

```sql
CREATE TABLE scope_generations (
  scope_id     uuid NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
  generation   bigint NOT NULL,
  targets      jsonb NOT NULL,        -- the targets this generation evaluates, agents resolved to ids
  state        text NOT NULL CHECK (state IN ('building', 'active', 'retired')),
  completed_at timestamptz,
  PRIMARY KEY (scope_id, generation)
);
CREATE UNIQUE INDEX scope_generations_active   ON scope_generations (scope_id) WHERE state = 'active';
CREATE UNIQUE INDEX scope_generations_building ON scope_generations (scope_id) WHERE state = 'building';

CREATE TABLE scope_members (
  scope_id      uuid   NOT NULL,
  generation    bigint NOT NULL,
  resource_type text   NOT NULL,      -- config | component | check | canary | playbook
  resource_id   uuid   NOT NULL,
  PRIMARY KEY (scope_id, generation, resource_type, resource_id),
  FOREIGN KEY (scope_id, generation) REFERENCES scope_generations ON DELETE CASCADE
);
CREATE INDEX scope_members_resource_idx ON scope_members (resource_type, resource_id);
```

- A Scope has at most one `active` generation, the one in effect, and at most one `building` generation, the one being computed. A generation's `targets` are a snapshot, so a Scope can be edited again while a generation built from its previous targets is still in effect.
- The primary key serves the listing predicate, driven from the Scope side, and the build. The secondary index serves single-resource checks, the row-side worker, and "who can read this resource".
- `retired` generations are invisible to enforcement. A cleanup job deletes their member rows in batches, then the generation row.
- Connections have no membership: a `read` rule's Scope selects them whole ([roles.md Section 3.1](../roles.md#31-the-read-action)), which stays an object policy.
- Views keep their `grants` column. Converging them onto `scope_members` is a later change.
- `resource_id` has no foreign key, since the five tables differ. Membership of a deleted resource is removed by the row-side worker ([Section 5.3](#53-row-side)).

### 3.1 Why a side table and not a column

|                              | `scope_members` table          | `scope_ids uuid[]` column on each table                                                  |
| ---------------------------- | ------------------------------ | ---------------------------------------------------------------------------------------- |
| Row-level security predicate | Indexed join                   | `&&` with a GIN index, no join                                                           |
| Row changes                  | Delete and insert for that row | Rewrite the array in the same update                                                     |
| Scope edit                   | Writes that Scope's rows only  | Updates every row whose membership changes, on `config_items` while scrapers write to it |
| Two generations at once      | Two sets of rows               | Not expressible without a second column                                                  |
| New protected type           | Nothing                        | A column and an index per table                                                          |

The column is faster to read. The table keeps an admin saving a Scope from contending with scrapers on the busiest table, keeps one Scope's build independent of the others, and lets a Scope's next generation be built beside the one in effect. Start with the table; add a column as a cache for one table if the join shows in profiles.

## 4. Evaluation

Membership is computed by one function, `MembersOf(targets, row types.ResourceSelectable) bool`, which applies [scopes.md Section 4](../scopes.md#4-membership) to one generation's `targets`: OR across the targets of the row's type, AND within a target, through `ResourceSelector.Matches`. `models.ConfigItem`, `Component`, `Check`, `Canary` and `Playbook` already implement `ResourceSelectable`.

The build, the row-side worker and the safety net use this function, and nothing else evaluates a Scope for `read`. `match_scope()` stops being used for Scopes ([Section 6](#6-enforcement)).

Every field in [scopes.md Section 5.1](../scopes.md#51-fields), with `fieldSelector` limited to the keys of Section 5.3, is a field of the row, and `agent` is resolved into the generation's `targets`, so every Scope can be materialised. The `rowFilterFields` allowlist is deleted. The constraint that replaces it is [scopes.md Section 4.3](../scopes.md#43-membership-is-decided-by-the-resource-alone): a selector may not depend on another row or on anything outside the row and the Scope. A future relationship-based selector would have to be rejected by `ValidateScope`.

## 5. Reconciliation

Two paths keep `scope_members` current: the **build** computes a Scope's new generation, and the **row-side worker** re-evaluates resources that changed. Both are asynchronous. [Section 5.5](#55-windows) states the windows.

### 5.1 Generations

`PersistScope`, `DeleteScope`, `DeleteStaleScope` in `db/scope.go`, and the re-validation of [scopes.md Section 7](../scopes.md#7-how-changes-take-effect), change generations in the same transaction that changes the Scope:

| Scope change                                                        | Generations                                                                                                                  |
| ------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Created valid, or its resolved targets change (incl. an `agent` resolving to another id, or becoming valid again) | A new `building` generation with the resolved targets. An existing `building` generation is `retired`: it's superseded, never completed |
| Only `description` or other fields that select nothing change       | None                                                                                                                         |
| Becomes invalid, or is deleted                                      | Every generation is `retired`                                                                                                |

Retiring on invalidation is what makes revocation immediate ([scopes.md Section 7.1](../scopes.md#71-membership-changes)): the next claim names no generation of the Scope, whether or not the Roles referencing it have been re-validated yet.

### 5.2 Build

A job builds `building` generations, up to `rbac.membership.builds` at a time (default 2), never in a request path. For each type the generation's targets select that the Scope doesn't select whole: stream the table in batches of 5,000 by primary key, evaluate each row, and insert members of the building generation. Then promote, in one transaction: the building generation becomes `active`, the previous active one `retired`, and `completed_at` is set.

A build whose generation is retired while it runs stops at its next batch.

The Scope's status carries `status.conditions[MembersReconciled]` with the active generation, its `completed_at`, and the member count per type, and `True` once the latest generation is active. The API returns the same. It doesn't affect `Ready` ([scopes.md Section 7.1](../scopes.md#71-membership-changes)).

### 5.3 Row-side

Rows are written by other processes: `config_items` by config-db, `checks` and `canaries` by canary-checker, `components` by topology scrapers. Mission Control can't hook those writes, so the database signals them:

```sql
CREATE SEQUENCE scope_members_pending_seq;
CREATE TABLE scope_members_pending (
  resource_type text NOT NULL,
  resource_id   uuid NOT NULL,
  seq           bigint NOT NULL DEFAULT nextval('scope_members_pending_seq'),
  queued_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (resource_type, resource_id)
);
```

A trigger on each of the five tables upserts into `scope_members_pending`, setting a new `seq` and `queued_at` on conflict, after an insert, after a delete, and after an update that changes a field a selector can read: the fields of [scopes.md Section 5.1](../scopes.md#51-fields), the `fieldSelector` keys of Section 5.3, and `deleted_at`. That list is closed, so the trigger's column list is too. Updates that touch none of them, which is most of config-db's traffic (`config`, `last_scraped_time`, timestamps), fire nothing.

A worker drains the pending table every `rbac.membership.drainInterval` (default 5 s). Per type, in one transaction:

1. Take the type's lock ([Section 5.4](#54-ordering)).
2. Read up to 10,000 entries with their `seq`.
3. Load those rows, deleted ones included, and every `active` and `building` generation that selects the type and isn't whole-type for it, from the database, not from a cache.
4. Evaluate each row against each of those generations, delete the rows' membership in those generations, and insert the matches. A deleted or soft-deleted row ends with no membership.
5. Delete the entries whose `seq` is unchanged. An entry re-queued since step 2 stays for the next drain.

### 5.4 Ordering

The build, the worker and the promotion each read a row's fields and write its membership in different transactions, so without ordering a decision based on an older snapshot can overwrite a newer one: a build batch reads a config while it's tagged `tenant=a`, the worker re-evaluates it after it's re-tagged and removes it, then the build batch commits and inserts it again.

Every transaction that writes `scope_members` for a type takes that type's transaction-level advisory lock first (`pg_advisory_xact_lock`), and reads rows and generations after taking it. So each one sees every decision committed before it:

- A row changed after a build batch read it is re-queued by the trigger, and the worker re-evaluates it after the batch commits, against the building generation too.
- The worker reads generations under the lock, so it sees a building generation created before it started, and the promotion can't happen in the middle of a drain.

The lock is held for one batch at a time: a build batch of 5,000 rows, or one drain of a type. Scrapers never take it: their writes only queue entries.

### 5.5 Windows

| Change                                  | Visible to row-filtered subjects                                                   | Until then                                                     |
| --------------------------------------- | ---------------------------------------------------------------------------------- | -------------------------------------------------------------- |
| Resource created, changed or deleted    | After the drain that takes its entry: one interval at normal load                  | Hidden: in no materialised Scope                               |
| Scope's resolved targets changed        | When its build is promoted: seconds for small tables, minutes for `config_items`   | The previous generation, exactly                               |
| Scope created                           | When its first build is promoted                                                   | Selects nothing                                                |
| Scope invalid or deleted                | Immediately ([Section 5.1](#51-generations))                                       |                                                                |
| Role or binding changed                 | Immediately; the claim changes, membership doesn't                                 |                                                                |

Under a backlog or a stopped worker, changed resources stay hidden longer; nothing is ever granted that neither the old nor the new state grants. A resource that changes constantly flickers out of filtered listings as it does. The metric `scope_members_pending_oldest_seconds` reports the oldest entry's age, and alerts above a threshold.

A narrowing edit to a Scope keeps granting the previous generation for the length of its build. This is deliberate: the alternative is granting nothing through the Scope while it builds. Revoking at once is done through the Role or binding, which takes effect on the next request.

Subjects with `all` on a type, and members, whose listings aren't filtered, see none of these windows.

### 5.6 Safety net

A job walks each type every 6 hours in batches, under the type's lock: it evaluates each row against every active generation, compares the result with the stored rows, and repairs any difference. Rows with a pending entry are skipped, since they're hidden and will be re-evaluated. Under the lock, nothing that's allowed to lag is in flight for the batch, so every difference is drift: it's counted in `scope_members_drift`, and any non-zero value is a bug in a reconcile path and alerts.

## 6. Enforcement

### 6.1 Claim

`rls.Payload` carries, per resource type, either `all: true` or a list of grants. Each grant is a set of Scope generations the row must be a member of, all of them:

```json
{
  "config": {
    "grants": [
      { "scopes": [{ "id": "<media-configs>", "generation": 7 }] },
      { "scopes": [{ "id": "<prod>", "generation": 3 }, { "id": "<tenant-a>", "generation": 12 }] }
    ]
  },
  "playbook": { "all": true }
}
```

`buildRLSPayload` produces it from `adapter.LoadBindingRules`, which resolves each rule's `Resource` to its Scopes, on every request, reading active generations at the same time:

1. A `read` rule without a constraint gives a grant of its Scope. A rule narrowed by a constraint ([rolebindings.md Section 3.2](../rolebindings.md#32-which-rules-a-constraint-narrows)) gives a grant of the rule's Scope and the constraint's `resource` Scope: the row must be in both.
2. A Scope that's whole-type for the type is left out of the grant: every row is in it. If that leaves the grant empty, the type is `all`. This is the same test collection-access.md Section 2.4 uses for `all`.
3. A Scope named twice in a grant appears once.
4. If any Scope left in the grant has no active generation, because its first build isn't promoted or it's invalid, the grant is dropped whole. Dropping only that Scope would widen the grant.
5. A type with neither `all` nor a grant is absent from the claim: `none`.

So a grant is never empty and never repeats a Scope, and `all` arises only from a whole-type grant.

Legacy Permissions with inline selectors (`addPermissionFilters`) are compiled into hidden Scopes keyed by Permission id, so they flow through the same path, and `match_scope()` leaves the predicate entirely ([Section 10](#10-open-questions)).

### 6.2 Row-level security policy

For `config_items`, and likewise for the other four tables:

```sql
USING (
  is_rls_disabled()
  OR coalesce((rls_claims() -> 'config' ->> 'all')::boolean, false)
  OR (
    NOT EXISTS (
      SELECT 1 FROM scope_members_pending p
      WHERE p.resource_type = 'config' AND p.resource_id = config_items.id
    )
    AND config_items.id IN (
      SELECT m.resource_id
      FROM rls_grants('config') g     -- (grant_no, scope_id, generation, size), parsed from the claim once per statement
      JOIN scope_members m
        ON m.scope_id = g.scope_id AND m.generation = g.generation AND m.resource_type = 'config'
      GROUP BY g.grant_no, g.size, m.resource_id
      HAVING count(*) = g.size
    )
  )
)
```

`count(*) = size` is exact because a grant names each Scope once, with one generation (Section 6.1). The `IN` lets the planner start from the Scope's members, so a sparse grant costs its member count rather than the table's size; [Section 7](#7-cost) covers dense grants.

### 6.3 Single-resource checks

`HasPermission` for `read` answers from the same facts the policy reads: the type is `all`, or the resource has no pending entry and is a member of every Scope of some grant, in the generations the claim names. The simplest way to guarantee they agree is for it to run the policy's membership subquery for the one id. Opening a resource and listing it then read the same stored fact, as [roles.md Section 3.1](../roles.md#31-the-read-action) requires, through the same windows.

Actions other than `read` keep evaluating selectors in `matchRule`, against the resource in hand, so they see a change at once. An operation that also checks `read` ([roles.md Section 4.3](../roles.md#43-what-an-operation-must-provide)), such as running a playbook on a config, needs both to pass, so while the two disagree it's refused, never allowed ([scopes.md Section 7.1](../scopes.md#71-membership-changes)).

### 6.4 Collection-level

collection-access.md derives `all`, `some` or `none` from grants, so it needs nothing from `scope_members`. Membership adds what the specification leaves to the design:

- `GET /scopes/{id}/members?type=config&limit=` for the Scope's admin page, from its active generation, and a dry-run of the same on a Scope being edited, evaluated in Go over a sample without storing.
- "Who can read this resource" in `rbac/access_review.go`: the resource's active memberships from `scope_members`, then the bindings whose rules name them.

## 7. Cost

Per resource change: one evaluation per active or building generation that selects its type. `ResourceSelector.Matches` on a tag selector is microseconds; a `fieldSelector` or `labelSelector` is comparable. With 200 Scopes and 10,000 changed rows in a drain, that's 2 million evaluations, roughly 2 to 5 seconds of one core, off the request path. A drain that takes longer than the interval delays the next and holds the type's lock that long; the batch size bounds both.

Per Scope change: one scan of each type it selects but not whole. A million-row `config_items` streams in batches of 5,000 in low minutes.

Per read: the policy's cost depends on how much of the table the subject's grants select, and isn't proportional to the page size in general:

- **Sparse grant** (a few rows of a large table): driven from `scope_members`, cost proportional to the grant's members, then the page.
- **Empty grant**: one index probe per Scope; the listing is empty.
- **Dense grant** (most of the table): the member set is as large as the table, and a per-row `EXISTS` on `scope_members_resource_idx` may plan better than the `IN`. Either form is correct; which one ships, or whether both are generated by grant size, is decided by measurement.

**To measure before accepting:** drain latency and CPU at the largest tenant's Scope count and peak change rate; `config_items` build time per Scope change; p95 of the Catalog listing for a row-filtered subject with a sparse, an empty and a dense grant, before and after. Budgets: the drain keeps up at twice the observed peak change rate; a Scope change on the largest tenant is promoted within 10 minutes; a filtered listing is no slower than the unfiltered listing of the same page.

## 8. Alternatives

- **Keep evaluating per row, and extend `match_scope()`.** Keeps two implementations in step by hand, and every listing stays a scan. This is what the field table formerly in roles.md existed to contain.
- **Array column per table** (as views do). Rejected for the resource tables for the reasons in [Section 3.1](#31-why-a-side-table-and-not-a-column); kept for views, which are rebuilt wholesale.
- **Compute each subject's member set at request time and cache it.** Per-user caches of resource ids don't scale with users times resources, and invalidation on every scrape defeats the cache.
- **A relationship store such as SpiceDB or OpenFGA.** The right shape, but a second datastore to run, and Postgres row-level security already does the lookup once the facts are rows.
- **Evaluate selectors in SQL triggers.** Puts the selector grammar back into plpgsql, which is the duplication being removed, and runs it inside the writers' transactions.
- **Grant the old and the new membership together during a build.** Simpler, with one generation per Scope, but grants resources neither version selects, for minutes. Rejected by [scopes.md Section 7.1](../scopes.md#71-membership-changes).

## 9. Rollout

1. Migration: `scope_generations`, `scope_members`, `scope_members_pending`, the five triggers, the Scope status fields. No behaviour change.
2. Backfill: a building generation for every valid Scope, built and promoted.
3. Shadow: with `rls.membership.shadow=true`, listings keep the current predicate, and the safety-net job also reports rows where the membership predicate and `match_scope()` disagree for each Scope the allowlist accepts. Run for a release.
4. Switch: `rls.membership.enabled=true` installs the new policies and claim, and `HasPermission` for `read` reads membership. The `rowFilterFields` allowlist is removed in the same release, which widens what a `read` rule's Scope may use to everything [scopes.md Section 5](../scopes.md#5-selector-language) allows.
5. Later: views converge; legacy Permission selectors are retired with Permissions.

What changes for users at the switch:

- **Roles that start granting.** A Role that's `Ready=False` only because a `read` rule's Scope used a field outside the allowlist becomes valid, and starts granting, without anyone touching it. Same for bindings whose constraint was refused for that reason. The release lists them before the switch, in its notes and in the shadow step's report, so they can be fixed or deleted first.
- **Windows.** The delays of [Section 5.5](#55-windows) appear, as [scopes.md Section 7.1](../scopes.md#71-membership-changes) allows.

## 10. Open questions

- **Drain interval, batch size and concurrent builds** are guesses until measured ([Section 7](#7-cost)).
- **Dense grants:** the `IN` form, the per-row `EXISTS` form, or both chosen by grant size ([Section 7](#7-cost)).
- **Hidden Scopes for Permissions** ([Section 6.1](#61-claim)): whether to compile them, or keep `match_scope()` for Permission filters alone until Permissions are retired. Compiling is one mechanism; keeping is less migration.
- **`fieldSelector` keys** per type ([scopes.md Section 5.3](../scopes.md#53-fields-each-type-supports)) are still to be listed; the trigger's column list follows from them.
- **`health` and `status` in Scopes.** Row-local, so allowed, but a Scope on `health=unhealthy` re-queues the resource on every health flip, and it's hidden from filtered listings until drained. Whether to allow them in `read` rules at all is a specification question for [roles.md](../roles.md).
- **Checks.** collection-access.md reads checks through their canary (flanksource/mission-control#3520). If that lands, `checks` needs no membership or trigger.
