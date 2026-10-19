# Deny RoleBindings

Implements `rolebindings.md`, Section 5, and `collection-access.md`, Section 2.5.

## Background

A deny has to hold at every place a grant is enforced: Casbin checks on one resource, Casbin checks on whole object types, and Postgres row-level security on listings. It can be enforced either by subtracting denies from grants before they reach those places, or by giving each place a deny of its own. We chose a deny at each place, because grants and denies name Scopes, and checks read each Scope's stored membership when a request is made: "every config in `prod` but not in `secrets`" isn't a Scope, so it can't be compiled into one grant without storing a membership of its own.

## Storage

`role_bindings.deny` is a boolean, `false` by default. The RoleBinding CRD and API carry it as `spec.deny`. A token binding never sets it: a stored token binding with `deny` is invalid.

## Casbin

The model's effect is `some(where (p.eft == allow)) && !some(where (p.eft == deny))`. A deny binding compiles like any binding, under its own principal and with its subjects grouped into it, with the `deny` effect:

- **Per-request rules** get the same condition over `r.obj.Membership` as allow rules. A request whose membership can't be read matches no rule of either kind. Every allow on a resource also needs membership, so the request is refused anyway.
- **Whole-type rules** (`create`, `update`, `delete`) are denied on the object of each type, e.g. `catalog`.
- **Read rules** are denied on the object of every type they select, entirely or in part. A string-object check, e.g. the middleware's `catalog`/`read`, is how an endpoint decides that a subject may read the whole type without filtering rows. Failing it sends a partially denied subject down the row-filtered paths that serve `some`.
- **Actions without a resource** are denied on their object, e.g. `events`.

### RoleBindings left out

Before narrowing, a deny binding drops the `roleBinding` type from each rule's resource Scope, so nothing compiles a deny on the `role-bindings` object. A RoleBinding is never an ABAC resource, so the per-request conditions can't reach one either. A rule left with no type is reported with reason `DenyOnRoleBindings`. Dropping the type, instead of refusing such a Role, lets a Role that also manages RoleBindings be bound as a deny for everything else.

## Row-level security

The claim gains a `deny` object of grants by type, in the same shape as the grants:

```json
{"config": [{"scope": "<all-configs>"}], "deny": {"config": [{"scope": "<secrets>"}]}}
```

- `rls_denying_scopes(kind)` returns the Scopes each deny grant needs, without whole-type Scopes, like `rls_required_scopes`. A malformed deny grant denies every row.
- `rls_row_denied(kind, id)` checks those against stored membership. `rls_matching_grants` yields nothing for a denied row. Every table's policy goes through it, so they all follow.
- `rls_grants_admit_row`, used by `WITH CHECK` and by rows not stored yet, checks the deny grants against the row's values, so a writer can't insert a denied row.
- The `checks` policy also admits a check through its canary. That path is refused for a denied check too.

A deny over a whole type also takes the type's grants away, so its answer is `none`. Every deny is still sent as a deny grant, even for a type with no grants of its own, since a row can be admitted another way, e.g. a check through its canary.

A request made with an access token gets its owner's deny grants unchanged. The token's bindings only narrow the grants.

## Alternatives considered

- **Subtract at compile time.** Rejected: see Background. It would also need a recompile whenever a Scope's membership changes, which allow rules avoid by naming Scopes.
- **Deny on Role rules.** Rejected by `rolebindings.md`, Section 5.2.
- **Deny everything while a deny binding isn't Ready.** Rejected by `rolebindings.md`, Section 5.5.
- **Exempt the shipped `admins` team from denies.** Rejected: a deny then couldn't guard against the commonest mistake, binding someone to `admin`. Leaving RoleBindings out keeps recovery just as certain, without exempting anyone.
