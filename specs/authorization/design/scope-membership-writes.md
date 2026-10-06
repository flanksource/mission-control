# Scope Membership Writes

Details how a Scope's membership is written, for `materialised-membership.md` ("Keeping it current").

## Background

A Scope's membership, `scope_targets` and `scope_members`, can be written by Mission Control when it saves the Scope, or by a trigger on `scopes`. This design uses a trigger. _Why:_ Postgres then writes all membership, for resources and Scopes alike, so a Scope written any way, through Mission Control, a migration or plain SQL, gets its membership in the same transaction. Deciding membership is already SQL (the one predicate and the rebuild); only turning a Scope's targets into rows was left outside it.

## Design

- **Writers.** Only triggers in Postgres write `scope_targets` and `scope_members`. Mission Control MUST NOT write them.
- **Validity.** Mission Control validates a Scope (`scopes.md` §6) when it's saved and on every policy reload. It rejects a Scope that's wrong on its own, and stores the rest with their error, if any, which is what `Ready` reports. The trigger MUST NOT decide validity; it follows the stored error. _Why:_ the API and CRD status need readable reasons before anything is written, and one validator can't disagree with itself.
- **Scope written.** A trigger on `scopes` runs on insert, and on update when `targets`, the error or `deleted_at` changes, in the writing transaction:
  1. A deleted Scope, or one stored with an error, has its membership cleared.
  2. Otherwise each target becomes a `scope_targets` row: `agent` resolved to an id, a trailing `*` in `name` to a name prefix, `tagSelector` and `labelSelector` to `key=value` maps. A whole-type target becomes one `scope_members` row with no `resource_id`. View targets are skipped.
  3. The Scope's members are rebuilt from those rows with the one predicate.

  A save that changes none of those columns runs nothing. _Why:_ a reconcile saves every Scope again on restart. A Scope deleted outright loses its membership in the same transaction.
- **Agent doesn't resolve.** The Scope's membership is cleared, and the write succeeds. _Why:_ an agent can be deleted between validation and the write. The Scope then selects nothing, as `scopes.md` §7 requires, and the next policy reload marks it `Ready=False`.
- **Target can't be converted.** The write fails. Through Mission Control, that's a failed save: `PersistFailed`, with the previous version in effect. _Why:_ it means validation passed a Scope the trigger can't build. Failing keeps `Ready=True` from ever describing a Scope without membership, and refuses a bad Scope written with plain SQL.
- **Agent written.** A trigger on `agents` runs on insert, on a name change and on a `deleted_at` change. It rebuilds every Scope that names the agent, by name or id, and isn't stored with an error, by steps 2 and 3. _Why:_ an agent deleted and re-registered under the same name between two policy reloads changes no Scope, so only this trigger sees the new id (`scopes.md` §5.4).
- **Lock.** The rebuild takes the membership lock exclusively inside the trigger, with the timeout and bounded retries of `materialised-membership.md` ("Lock"). Out of retries, the write fails.
- **Scopes without membership.** Every Scope that's neither deleted nor stored with an error has membership before Mission Control accepts requests; duty's migrations build any that have neither targets nor members. _Why:_ a grant through a Scope with no rows is refused.

Example: saving

```yaml
spec:
  targets:
    - config: { name: "web-*", agent: prod, tagSelector: "env=prod,team=web" }
    - playbook: { name: "*" }
    - view: { name: dashboards }
```

writes, in the same transaction:

```
scope_targets  (config, name_prefix='web-', agent_id=<id of prod>, tags={"env":"prod","team":"web"})
scope_members  (playbook, resource_id=NULL)
scope_members  (config, <id>) for every config the target matches
```

## Other choices

- **Mission Control converts targets and calls the rebuild in its save transaction.** Membership is then written in two places. A Scope written outside Mission Control has none until something re-syncs it, and the save needs a check that the Scope wasn't saved again between resolving its targets and rebuilding.
- **The trigger also validates, and Mission Control doesn't.** The API and CRD status would only get a database error, and every rule of `scopes.md` §6 would have to be written in plpgsql.
- **Agent changes handled by the policy reload.** Lags by a reload, and writes membership from outside a trigger.

## FAQ

**What does a Scope written with plain SQL get?**
Its membership, in the same transaction. Its `Ready` catches up on the policy reload that the write's notification schedules.

**What does Mission Control still do for a Scope?**
Validate it, reject it or record why it isn't in effect, and save the row. Nothing about its membership.
