# Scope Membership Writes

Details how a Scope's membership is written, for `materialised-membership.md` ("Keeping it current").

## Background

A Scope's membership, `scope_targets` and `scope_members`, can be written by Mission Control when it saves the Scope, or by a trigger on `scopes`. This design has Mission Control write it. _Why:_ Mission Control already validates the Scope and resolves its agents (`scopes.md` §5.4, §6) before saving it, so turning the targets into rows in the same transaction keeps one place that reads a Scope. Scopes are only written through Mission Control, from a CRD or its API, so no Scope is saved without it.

Resources are the opposite: other processes, such as config-db and canary-checker, write them directly, so a resource's membership is written by a trigger on its table (`materialised-membership.md`). Both directions match with the same SQL predicate.

## Design

- **Writers.** Mission Control writes `scope_targets`, and the members of the Scope it saves. Triggers on resource tables write the membership of the resources they write. Nothing else writes either table.
- **Scope saved.** In the save transaction, after validation:
  1. A Scope stored with an error, including an `agent` that isn't registered, has its membership cleared. Its targets aren't written. _Why:_ the Scope selects nothing (`scopes.md` §7), so there's no target row to build, and no agent id to put in one.
  2. Otherwise each target becomes a `scope_targets` row: `agent` resolved to the id of the agent with that name, a trailing `*` in `name` to a name prefix, `tagSelector` and `labelSelector` to `key=value` maps. A whole-type target becomes one `scope_members` row with no `resource_id`. View targets are skipped.
  3. The Scope's members are rebuilt from those rows with the one predicate.

  A save whose targets and resolved agents haven't changed rebuilds nothing. _Why:_ a reconcile saves every Scope again on restart.
- **Scope deleted.** Its membership is cleared in the transaction that deletes it.
- **Agent registered, renamed or deleted.** Postgres notifies Mission Control, which schedules a policy reload. The reload validates every Scope again, and rebuilds those whose `agent` now resolves to a different id, or no longer resolves. _Why:_ agents are set up once and rarely change, so a lag of one reload costs nothing a user would notice (`scopes.md` §7.1), and needs no trigger that resolves names.
- **Target can't be converted.** The save fails: `PersistFailed`, with the previous version in effect. _Why:_ validation passed a Scope that can't be built, and failing keeps `Ready=True` from ever describing a Scope without membership.
- **Lock.** The rebuild takes the membership lock exclusively, with the timeout and bounded retries of `materialised-membership.md` ("Lock"). Out of retries, the save fails.
- **Scopes without membership.** Before accepting requests, Mission Control builds every Scope that's neither deleted nor stored with an error and has neither targets nor members. If one can't be built, it doesn't start. _Why:_ a grant through a Scope with no rows is refused.

Example: saving

```yaml
spec:
  targets:
    - config: { name: "web-*", agent: prod, tagSelector: "env=prod,team=web" }
    - type: playbook
    - view: { name: dashboards }
```

writes, in the same transaction:

```
scope_targets  (config, name_prefix='web-', agent_id=<id of prod>, tags={"env":"prod","team":"web"})
scope_members  (playbook, resource_id=NULL)
scope_members  (config, <id>) for every config the target matches
```

## Other choices

- **Triggers on `scopes` and `agents` write all membership.** Postgres would then write membership for Scopes and resources alike, and a Scope written with plain SQL would get it too. Not taken: converting targets and resolving agents would be written again in plpgsql beside Mission Control's validation, for Scopes that are only written through Mission Control and agents that rarely change.
- **The trigger also validates, and Mission Control doesn't.** The API and CRD status would only get a database error, and every rule of `scopes.md` §6 would have to be written in plpgsql.
- **Resolve `agent` inside the predicate.** The name would be looked up on every resource write, and an agent change would still need the Scopes naming it rebuilt.

## FAQ

**What does a Scope written with plain SQL get?**
Nothing until the next policy reload, which validates it and builds its membership. Writes to these tables through `/db` are refused.

**What does Mission Control do for a Scope?**
Validate it, reject it or record why it isn't in effect, save it, and write its membership in the same transaction.
