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

| Field         | Type   | Required                  | Meaning                                                  |
| ------------- | ------ | ------------------------- | -------------------------------------------------------- |
| `role`        | string | Yes                       | Name of a Role in the binding's namespace (`roles.md`).  |
| `subjects`    | object | Yes, at least one subject | Who gets the Role (Section 2).                           |
| `constraint`  | object | No                        | Narrows the Role's rules for these subjects (Section 3). |
| `description` | string | No                        | Explanatory text; no effect.                             |


A RoleBinding MUST have a namespace, and its Role and constraint Scopes must be in it (`overview.md`, "Namespaces"). Subjects can match resources in any namespace (Section 2.5).

## 2. Subjects

`subjects` is an object. Every field is optional, but at least one subject MUST be given. A subject matched by several fields gets the Role once.

| Field                                                              | Type            | Selects                                                              |
| ------------------------------------------------------------------ | --------------- | -------------------------------------------------------------------- |
| `people`                                                           | list of strings | Mission Control users, by email only                                 |
| `teams`                                                            | list of strings | Every member of the teams, by team name                              |
| `agents`                                                           | list of strings | Agents, by name (Section 2.3)                                        |
| `oidc`                                                             | list of objects | Users of an external identity provider, by the claims in their token |
| `playbooks`, `notifications`, `topologies`, `scrapers`, `canaries`, `plugins` | list of objects | Those resources, when they act on their own               |

A person is a Mission Control user: someone who signs in through Mission Control's own authentication. Users of an external identity provider aren't people for the purpose of `people`; they're selected by `oidc` only (Section 2.4). Agents aren't people either; they're selected by `agents` (Section 2.3).

### 2.1 People

```yaml
subjects:
  people:
    - alice@example.com
    - bob@example.com
```

Each entry MUST be a person's email address, never their name, or `*` alone to select every person, e.g. to bind `viewer` to everyone in the organisation.

### 2.2 Teams

```yaml
subjects:
  teams:
    - platform
    - sre
```

Each entry is a team name. Members get the Role through their team, so joining or leaving the team changes who has it.

### 2.3 Agents

```yaml
subjects:
  agents:
    - eu-cluster
```

Each entry is an agent's name, or `*` alone to select every agent. Agents aren't people and can't be in a team, so this is the only way to select them.

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

See `external-identity-providers.md`.

### 2.5 Resources

Some resources act on their own and need permissions too, e.g. a playbook calling Mission Control while it runs, a notification reading the resources it reports on, or a plugin using a connection's credentials. These can be subjects:

| Field | Selects |
|---|---|
| `playbooks` | Playbooks |
| `notifications` | Notifications |
| `topologies` | Topologies |
| `scrapers` | Config scrapers |
| `canaries` | Canaries |
| `plugins` | Plugins, by the namespace and name of their Plugin resource. They can only be granted `read` and `connection:use` on connections (`roles.md`, Section 4.4) |

```yaml
subjects:
  playbooks:
    - namespace: default
      name: cleanup-pods
  notifications:
    - namespace: monitoring
  scrapers:
    - name: "*"
  plugins:
    - namespace: mission-control
      name: kubernetes-logs
```

Each entry is an object with `namespace` and `name`, and MUST set at least one of them; an empty entry is rejected. The values follow the same rules as in a Scope (`scopes.md`, Section 5.2):

- `name` is one exact value, or `*` as the whole value to match any name.
- `namespace` is one exact value. To match any namespace, omit it. `namespace: "*"` is rejected.
- Prefixes, suffixes, lists and exclusions are rejected in both.

So `name: "*"` alone selects every resource of that kind, and `namespace: monitoring` alone selects every resource of that kind in `monitoring`. Here: the `cleanup-pods` playbook, every notification in `monitoring`, every scraper, and the `kubernetes-logs` plugin in `mission-control`.

Subjects are matched by namespace and name, not by reference to a specific object, so they can match resources in any namespace, not only the binding's.

Editors of a resource’s executable definition are trusted with the permissions and credentials available to its execution.

Since resource subjects are matched this way, a binding also reaches resources created later that match it, and whoever can create one gets the binding's Role for what it does:

- `notifications: [{namespace: monitoring}]` grants its Role to every notification in `monitoring`. Anyone who can create a notification there can make one that uses it.
- `name: "*"` does the same across every namespace.
- An exact name not yet in use is taken by whoever first creates a resource with that name.

Bind by exact namespace and name wherever the Role is sensitive, e.g. `connection:use` on a connection that pages people, or anything bound to `admin`. _Why it's allowed:_ a playbook or notification's access is a property of where it lives, like a Kubernetes pod using any service account in its namespace. Granting by namespace is how a team gives its own playbooks access without a binding per playbook. The binding is still the grant: creating a resource that matches no binding gets it nothing, whatever connections or resources it names (`roles.md`, Section 4.4).

## 3. Constraint

A constraint lets one Role serve many groups, each limited to its own resources. Given this Role:

```yaml
kind: Role
metadata:
  name: production-runner
  namespace: default
spec:
  rules:
    - name: run-monitoring
      action: playbook:run
      resource:
        scopeRef: monitoring-playbooks
      target:
        scopeRef: production-configs
    - name: approve-monitoring
      action: playbook:approve
      resource:
        scopeRef: monitoring-playbooks
      target:
        scopeRef: production-configs
```

this binding limits both rules to tenant A's configs:

```yaml
kind: RoleBinding
metadata:
  name: tenant-a-runners
  namespace: default
spec:
  role: production-runner
  subjects:
    oidc:
      - provider: oipa
        match: "claims.tenant == 'a'"
  constraint:
    target:
      scopeRef: tenant-a
```

Tenant A's users can run and approve monitoring playbooks on the configs that are in both `production-configs` and `tenant-a`. A binding for tenant B does the same with `tenant-b`. Running a playbook on a config also checks `read` on the config (`roles.md`, Section 4.3), so they need a `read` grant too; Section 3.3 adds one.

`constraint` is an object with the shape of a rule:

| Field               | Type   | Required | Meaning                                                                                 |
| ------------------- | ------ | -------- | --------------------------------------------------------------------------------------- |
| `resource.scopeRef` | string | No       | A Scope, in the binding's namespace, the resource of every operation must also be in.   |
| `target.scopeRef`   | string | No       | A Scope, in the binding's namespace, the target of every operation must also be in.     |

At least one of them MUST be set: a `constraint` that sets neither is rejected. A missing `constraint` and `null` both mean no constraint, and the binding grants the Role's rules as written.

A constraint applies to every rule of the Role, one side at a time:

- With `resource`, an operation's resource must be in the rule's `resource` Scope **and** the constraint's.
- With `target`, an operation's target must be in the rule's `target` Scope **and** the constraint's.
- A side the constraint doesn't set isn't narrowed. A side the rule's action can't take, e.g. `target` on a `read` rule, is skipped for that rule.

Section 3.2 says exactly which rules a constraint narrows, and what happens to the ones it can't.

### 3.1 The whole Role is granted

A binding always grants its whole Role. A constraint doesn't choose among its rules: it has no list of rules, and names none. So:

- The binding says which Role is granted, and the constraint only says where. Which actions are granted is still read from the Role alone (`overview.md`: actions only live in Roles).
- A rule added to the Role later is narrowed like the others as soon as the Role is stored, without the binding being updated. If the constraint can't narrow it, it doesn't apply through the binding, and the binding reports it (Section 3.2). It's never granted as written.
- Renaming a rule doesn't affect the Role's bindings.

To grant only some rules of a Role, put them in a Role of their own and bind that.

#### Why not one constraint per rule

A list of constraints, each naming the rule it narrows, could narrow two rules differently in one binding. But the binding then has to decide what happens to the rules it doesn't name, and both answers are bad:

- **They're granted as written.** A rule added to the Role reaches every tenant un-narrowed until every binding is updated, and nothing reports it. The bindings can't be updated first either: a constraint naming a rule the Role doesn't have yet is invalid, so the binding would grant nothing in the meantime.
- **They aren't granted.** The binding then picks rules by name, so every rule added to the Role needs every binding edited, and what's granted is only found by comparing the binding's list with the Role's.

Kubernetes and GCP IAM narrow a binding as a whole too: a RoleBinding limits a ClusterRole to its namespace, and an IAM binding's condition applies to every permission of its role. Neither lets a binding pick rules of a role.

### 3.2 Which rules a constraint narrows

A constraint can't widen a rule: it narrows each input to the resources in both Scopes, and a resource is in the constraint's Scope by the usual membership rule (`scopes.md`, Section 4). For each rule, and each side the constraint sets:

| The rule's input on that side          | Types both Scopes select | Effect on the rule                                     |
| -------------------------------------- | ------------------------ | ------------------------------------------------------ |
| The action takes no such input         |                          | The side is skipped for this rule.                     |
| The action takes it, the rule omits it |                          | The rule doesn't apply through this binding.           |
| Present                                | None                     | The rule doesn't apply through this binding.           |
| Present                                | One or more              | The input is narrowed to the resources in both Scopes. |

`read` takes no target (`roles.md`, Section 2), so a constraint's `target` says nothing about a `read` rule and is skipped. `playbook:run` takes a target, so a `playbook:run` rule written without one isn't exempt from a `target` constraint: it allows exactly the runs that have no target (`roles.md`, Section 4.1), its set of allowed targets is empty, and the empty set narrowed by the constraint's Scope is still empty. The constraint can't fill the target in instead, since that would let the rule cover runs on those targets, which the Role never granted. So under a `target` constraint, a targetless rule of an action that takes a target applies to nothing, whatever the constraint's `resource` side does. A playbook that acts on no resource, like one that creates a namespace, can't be granted through a binding with a `target` constraint; it needs a binding without one.

"Types both Scopes select" are the types the input accepts (`roles.md`, Section 2) that a target of the rule's Scope and a target of the constraint's Scope both select. Two Scopes with a type in common can still select no resource in common, e.g. configs in two different namespaces. That's an empty set like any other (`scopes.md`, Section 4.1), and isn't reported.

For `read`, a constraint's Scope narrows a type to all of it when it has a whole-type target of the type, whatever its other targets, since targets combine with OR (`scopes.md`, Section 4). Such a rule doesn't need row-level security for that type, and `collection-access.md`, Section 2.4 counts it as whole-type by the same test.

Two rules close the gaps:

- **A rule that no side narrows doesn't apply through the binding.** That's a `read` rule under a constraint that only sets `target`. Leaving it as written would grant the subjects every resource of the rule, the leak Section 3.1 rules out. So a constraint never leaves a rule un-narrowed: either it narrows the rule, or the rule doesn't apply.
- **A type the input can't carry is ignored for that input.** A Scope of tenant A's configs and playbooks can be a constraint's `target` even though a target is never a playbook. Unlike a rule, a constraint may use part of a Scope: a rule's Scope is the grant, so a type the action can't use means the rule is wrong, while a constraint's Scope is a boundary, and a boundary that covers more than one input needs is harmless. This lets one Scope per tenant serve every input of every Role.

The narrowed input must still be enforceable for the rule's action, by the same requirements the rule's own Scope meets (`roles.md`, Sections 2 and 3.1). A constraint isn't a way around them. They're checked on the constraint's targets of the types both Scopes select, and only those: a target of another type never narrows the rule, so it's never checked against the rule's action. The playbook target in `tenant-a` (Section 3.3) is never checked against `read`, although `read` accepts playbooks, because `read-production`'s Scope selects no playbook. Where the checked targets don't meet the requirements, the rule doesn't apply through the binding:

- **`create`, `update`, `delete`.** These are only checked on all resources of a type (`roles.md`, Section 2), so a constraint can't narrow them within a type: the checked targets MUST be whole-type targets. It can still drop types: on a rule whose Scope selects every config and every component, a constraint whose Scope selects every config grants the action on configs only.
- **`read`.** Any checked target is accepted, since membership is decided by the resource alone (`scopes.md`, Section 4.3), except that connections MUST be whole-type targets (`roles.md`, Section 3.1). If a checked target isn't a whole-type target, the rule needs row-level security even when its own Scope doesn't: while it's off, the rule doesn't apply through the binding, with reason `RowLevelSecurityRequired`. It applies again when row-level security is enabled, without the binding being re-applied.

#### Reporting

A binding reports the rules that don't apply through it. Its `AllRulesApply` condition is `True` when every rule applies, and `False` with a message naming each rule that doesn't and why. The Role's status lists the bindings whose `AllRulesApply` is `False` (`roles.md`, Section 6), and the response to a Role update through the API carries the same list. A binding with that condition is `Ready=True` while at least one rule applies through it, and `Ready=False` once none does (Section 4).

A rule the constraint can't narrow is reported rather than failing the binding, for two reasons. Rules are added to the Role, and whoever adds one can't see its bindings: failing the binding would turn a rule added to a shared Role into an outage for every tenant. Reporting keeps what worked working, and still never grants as written. The price is that what a binding grants isn't readable from its Role alone, which is why the Role shows it too.

### 3.3 Rules of different shapes

One constraint serves a Role whose rules differ in shape, as long as its Scopes cover every input. This Role reads production configs and runs monitoring playbooks on them:

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

`tenant-a` selects tenant A's configs and the monitoring playbooks:

```yaml
kind: Scope
metadata:
  name: tenant-a
  namespace: default
spec:
  targets:
    - config:
        tagSelector: tenant=a
    - playbook:
        namespace: monitoring
```

and this binding sets it on both sides:

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
  constraint:
    resource:
      scopeRef: tenant-a
    target:
      scopeRef: tenant-a
```

| Rule              | `resource`                                         | `target`                                       |
| ----------------- | -------------------------------------------------- | ---------------------------------------------- |
| `read-production` | Configs in `production-configs` and `tenant-a`     | Skipped: `read` takes no target                |
| `run-monitoring`  | Playbooks in `monitoring-playbooks` and `tenant-a` | Configs in `production-configs` and `tenant-a` |

Tenant A's users read their production configs and run monitoring playbooks on them. The playbook target in `tenant-a` is ignored on the `target` side, since a target is never a playbook. Had `tenant-a` selected configs only, `run-monitoring` would share no type with it on the `resource` side and wouldn't apply.

With `target` alone, `read-production` would be narrowed on no side and wouldn't apply (Section 3.2). Tenant A could then run nothing either, since a run checks `read` on its target (`roles.md`, Section 4.3). The binding would report `read-production`: `AllRulesApply=False` is the first thing to check when a constrained binding grants less than expected.

To narrow two rules differently, put them in two Roles and bind each with its own constraint (Section 5).

## 4. How changes take effect

A binding is validated against what it references: its Role, and the Scopes its constraint names. Nothing is ever validated against the binding.

A binding that's wrong on its own is rejected (`overview.md`, "Rejected or not in effect"): no subjects, a `people` entry that isn't an email or `*`, an `oidc.match` that doesn't compile or doesn't return a bool, an empty or wildcard-namespace resource subject, or a `constraint` that sets neither `resource` nor `target`. Everything else is checked against the Role and Scopes, as follows.

- A binding takes effect only once its Role exists and is valid. Until then it's `Ready=False` with the reason and none of its rules apply. There is no previous version to fall back to; the binding is whatever was last written.
- Its rules apply as the constraint narrows them (Section 3.2). When the Role or a Scope the constraint names changes, is deleted or becomes invalid, or row-level security is turned on or off, each rule is checked again. A Scope that's gone or invalid selects nothing (`scopes.md`, Section 7), so no rule applies through the binding while that holds. A rule the constraint can't narrow doesn't apply and is reported; the others keep applying.
- A binding is `Ready=False` when its Role is missing or invalid, when a Scope its constraint names is missing or invalid, or when none of the Role's rules applies through the binding. The reason names the cause.
- It becomes `Ready=True` again, without being re-applied, as soon as the cause is gone.

So a Role and its bindings can be changed in any order. A constraint names no rule, so rules can be added, renamed or removed without touching the bindings: an added rule applies, already narrowed, as soon as the Role is stored, or is reported if the constraint can't narrow it.

## 5. Future work

A constraint could choose its Scope from the subject's claims, so one binding serves every tenant:

```yaml
constraint:
  target:
    scopeRef: tenant-{{ claims.tenant }}
```

Selectors would still only live in Scopes that were reviewed on their own. This isn't supported yet.

A constraint could also narrow one rule further than the others, within what it already allows for the whole binding. Until then, rules that need different narrowing go in different Roles.
