# RoleBinding Specification

## 1. What a RoleBinding is

A RoleBinding gives a Role to subjects: people, teams, users of an external identity provider, and a few kinds of resources.

```yaml
apiVersion: mission-control.flanksource.com/v1
kind: RoleBinding
metadata:
  name: staging-operators
  namespace: default
spec:
  description: Platform team operates staging
  role: staging-operator
  subjects:
    people:
      - alice@example.com
    teams:
      - platform
```

| Field         | Type            | Required                  | Meaning                                                 |
| ------------- | --------------- | ------------------------- | ------------------------------------------------------- |
| `role`        | string          | Yes                       | Name of a Role in the binding's namespace (`roles.md`). |
| `subjects`    | object          | Yes, at least one subject | Who gets the Role (Section 2).                          |
| `constraints` | list of objects | No                        | Narrow the Role's rules for these subjects (Section 3). |
| `description` | string          | No                        | Explanatory text; no effect.                            |

`role` never names a built-in role, since no Role can be named after one (`roles.md`, Section 1).

A RoleBinding MUST have a namespace. `role` names a Role in that namespace, and constraints name Scopes in that namespace (Section 3): a binding can't grant a Role, or narrow it with a Scope, from another namespace (`overview.md`, "Namespaces"). Subjects aren't references, and can match resources in any namespace (Section 2.5).

## 2. Subjects

`subjects` is an object. Every field is optional, but at least one subject MUST be given. A subject matched by several fields gets the Role once.

| Field                                                              | Type            | Selects                                                              |
| ------------------------------------------------------------------ | --------------- | -------------------------------------------------------------------- |
| `people`                                                           | list of strings | Mission Control users, by email only                                 |
| `teams`                                                            | list of strings | Every member of the teams, by team name                              |
| `roles`                                                            | list of strings | Every user, every guest, or every agent (Section 2.3)                |
| `oidc`                                                             | list of objects | Users of an external identity provider, by the claims in their token |
| `playbooks`, `notifications`, `topologies`, `scrapers`, `canaries` | list of objects | Those resources, when they act on their own                          |

A person is a Mission Control user: someone who signs in through Mission Control's own authentication. Users of an external identity provider aren't people for the purpose of `people`; they're selected by `oidc` only (Section 2.4). Agents aren't people either; use `roles: [agent]` (Section 2.3).

### 2.1 People

```yaml
subjects:
  people:
    - alice@example.com
    - bob@example.com
```

Each entry MUST be a person's email address, never their name. There's no wildcard: to select every Mission Control user, use `roles: [everyone]` (Section 2.3).

### 2.2 Teams

```yaml
subjects:
  teams:
    - platform
    - sre
```

Each entry is a team name. Members get the Role through their team, so joining or leaving the team changes who has it.

### 2.3 Built-in roles

```yaml
subjects:
  roles:
    - guest
```

`roles` selects subjects by their built-in role (`roles.md`, Section 7). Only three values are allowed, each because nothing else can select those subjects:

- `everyone`: every Mission Control user, guests and agents included. `people` has no wildcard, so this is the only way to select every user, e.g. to bind a deny rule that applies to all of them.
- `guest`: every guest. Guests see only what's shared with them (`roles.md`, Section 7.3); this shares with all of them at once, without a team to keep in step with who's invited.
- `agent`: every agent. Agents aren't people and can't be in a team, so this is the only way to select them.

`viewer`, `editor`, `commander` and `responder` are rejected. A team selects the same people, and these roles inherit from each other (`roles.md`, Section 7.2), so binding to one would also reach every role that inherits it, which a reader of the binding can't see. `commander` and `responder` aren't defined yet either.

`admin` is rejected too, since admins can already do everything. No binding applies to an admin, whether it names them in `people` or through `teams`: no rule applies to admins, deny rules included (`overview.md`, "Default access").

Users of an external identity provider have no built-in role, so no value selects them (Section 2.4).

### 2.4 External identity provider users

```yaml
subjects:
  oidc:
    - provider: oipa
      match: "'operators' in claims.groups && claims.tenant == 'a'"
```

Each entry is an object:

| Field      | Type   | Required | Meaning                                                                           |
| ---------- | ------ | -------- | --------------------------------------------------------------------------------- |
| `provider` | string | Yes      | Name of an `ExternalIdentityProvider`. Only tokens from it are considered.        |
| `match`    | string | Yes      | A CEL expression that returns a bool. `claims` holds the verified token's claims. |

- `claims` is a map of every claim in the token. Values keep their type: `claims.groups` is a list, `claims.tenant` a string.
- An expression that fails, e.g. on a missing claim, doesn't match.
- A `match` that doesn't compile, or doesn't return a bool, is rejected.
- The token is matched on every request, so a user loses the Role as soon as their token stops matching.

The `ExternalIdentityProvider` says which tokens are trusted:

```yaml
apiVersion: mission-control.flanksource.com/v1
kind: ExternalIdentityProvider
metadata:
  name: oipa
  namespace: mission-control
spec:
  issuer: https://auth.oipa.example.com
  audience: https://mission-control.example.com
  claims:
    username: sub
    name: name
    email: email
```

A user who signs in through an identity provider only gets what their RoleBindings grant. They get no built-in access. See `external-identity-providers.md`.

### 2.5 Resources

Some resources act on their own and need permissions too, e.g. a playbook calling Mission Control while it runs, or a notification reading the resources it reports on. These can be subjects:

| Field | Selects |
|---|---|
| `playbooks` | Playbooks |
| `notifications` | Notifications |
| `topologies` | Topologies |
| `scrapers` | Config scrapers |
| `canaries` | Canaries |

```yaml
subjects:
  playbooks:
    - namespace: default
      name: cleanup-pods
  notifications:
    - namespace: monitoring
  scrapers:
    - name: "*"
```

Each entry is an object with `namespace` and `name`, and MUST set at least one of them; an empty entry is rejected. The values follow the same rules as in a Scope (`scopes.md`, Section 5.2):

- `name` is one exact value, or `*` as the whole value to match any name.
- `namespace` is one exact value. To match any namespace, omit it. `namespace: "*"` is rejected.
- Prefixes, suffixes, lists and exclusions are rejected in both.

So `name: "*"` alone selects every resource of that kind, and `namespace: monitoring` alone selects every resource of that kind in `monitoring`. Here: the `cleanup-pods` playbook, every notification in `monitoring`, and every scraper.

Subjects are matched by namespace and name, not by reference to a specific object, so they can match resources in any namespace, not only the binding's.

Editors of a resource’s executable definition are trusted with the permissions and credentials available to its execution.

## 3. Constraints

Constraints let one Role serve many groups, each limited to its own resources. Given this Role:

```yaml
kind: Role
metadata:
  name: production-operator
  namespace: default
spec:
  rules:
    - name: read-production
      action: read
      resource:
        scopeRef: production-configs
    - name: run-monitoring
      action: playbook:run
      resource:
        scopeRef: monitoring-playbooks
      target:
        scopeRef: production-configs
```

this binding limits both rules to tenant A's configs:

```yaml
kind: RoleBinding
metadata:
  name: tenant-a-operators
  namespace: default
spec:
  role: production-operator
  subjects:
    oidc:
      - provider: oipa
        match: "claims.tenant == 'a'"
  constraints:
    - rule: read-production
      resource:
        scopeRef: tenant-a
    - rule: run-monitoring
      target:
        scopeRef: tenant-a
```

Tenant A's users can read configs in both `production-configs` and `tenant-a`, and run monitoring playbooks on configs in both. A binding for tenant B does the same with `tenant-b`.

Each constraint is an object:

| Field               | Type   | Required | Meaning                                                                        |
| ------------------- | ------ | -------- | ------------------------------------------------------------------------------ |
| `rule`              | string | Yes      | Name of a rule of the Role. Each rule can be constrained at most once.         |
| `resource.scopeRef` | string | No       | A Scope, in the binding's namespace, the operation's resource must also be in. |
| `target.scopeRef`   | string | No       | A Scope, in the binding's namespace, the operation's target must also be in.   |

The operation's resource must be in the rule's Scope **and** the constraint's; the same goes for the target.

### 3.1 Constraints only narrow

A constraint can't widen a rule. So it MUST NOT:

- **Name a deny rule.** Narrowing a deny lets through what it used to block.
- **Set `target` on a rule without one.** That would let a rule that only covers operations without a target cover operations on these targets.

A constraint narrows the rule's Scope to the resources in both, so its Scope MUST meet everything the rule's own Scope must meet for that action (`roles.md`, Sections 2, 3 and 3.1). A constraint isn't a way around those requirements:

- **Types.** Every type the constraint's Scope selects MUST be accepted by the input it narrows, and at least one of them MUST also be selected by the rule's Scope for that input. Otherwise nothing is in both, and the constraint would grant nothing without saying so.
- **`create`, `update`, `delete`.** These are only checked on all resources of a type (`roles.md`, Section 2), so a constraint can't narrow them within a type. A constraint on such a rule MUST use whole-type targets only. It can still drop types: on a rule whose Scope selects every config and every component, a constraint whose Scope selects every config grants the action on configs only.
- **`read`.** The constraint's Scope MUST only use fields row-level security can match (`roles.md`, Section 3.1). A constraint with a target that isn't a whole-type target needs row-level security, even when the rule's Scope doesn't: while it's off, the binding is `Ready=False` with reason `RowLevelSecurityRequired` and grants nothing (Section 4). It becomes effective when row-level security is enabled, without being re-applied.

A binding that breaks any of these is invalid, like one whose constraint names a rule the Role doesn't have (Section 4). Since these depend on the Role and the Scopes, which can change after the binding is written, such a binding is stored `Ready=False`, not rejected.

### 3.2 Only listed rules are granted

- Without constraints, the binding grants every rule of the Role. An empty list, `null` and a missing `constraints` field all mean no constraints.
- With at least one constraint, it grants only the allow rules it lists. To grant a rule unchanged, list it without a Scope:

  ```yaml
  constraints:
    - rule: read-production
      resource:
        scopeRef: tenant-a
    - rule: run-monitoring
  ```

- Deny rules always apply while the binding is in effect, listed or not.

This way, a rule added to the Role later isn't granted to tenant A until their binding lists it. Otherwise it would reach every tenant unrestricted.

## 4. Changes

A binding is validated against what it references: its Role, and the Scopes its constraints name. Nothing is ever validated against the binding.

A binding that's wrong on its own is rejected (`overview.md`, "Rejected or not in effect"): no subjects, a `people` entry that isn't an email, a built-in role that can't be bound, an `oidc.match` that doesn't compile or doesn't return a bool, an empty or wildcard-namespace resource subject, or a rule constrained twice. Everything else is checked against the Role and Scopes, as follows.

- A binding takes effect only once its Role exists and every constraint is valid against it. Until then it's `Ready=False` with the reason and grants nothing. There is no previous version to fall back to; the binding is whatever was last written.
- A binding stays effective only while that holds. When its Role or a Scope a constraint names changes, is deleted or becomes invalid, or row-level security is turned off, the binding is checked again. If a constraint no longer fits, because its rule was removed, renamed, turned into a deny rule, or lost the target the constraint narrows, or if a Scope no longer meets what the rule's action requires (Section 3.1), or if the Role or a Scope is gone or invalid (`scopes.md`, Section 7), or if row-level security is off while a constraint needs it (Section 3.1), the binding is `Ready=False` with the reason and has no effect: it applies none of its Role's rules, allow or deny, not the broken rule and not the others. An invalid Role likewise applies none of its rules to any binding (`roles.md`, Section 6).
- It becomes effective again, without being re-applied, as soon as its Role and Scopes fit its constraints and row-level security is on if a constraint needs it.

So a Role and its bindings can be changed in any order. Renaming a rule and updating the binding that names it converge once both are stored, whichever is applied first.

## 5. Future work

A constraint could choose its Scope from the subject's claims, so one binding serves every tenant:

```yaml
constraints:
  - rule: read-production
    resource:
      scopeRef: tenant-{{ claims.tenant }}
```

Selectors would still only live in Scopes that were reviewed on their own. This isn't supported yet.
