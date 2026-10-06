# Role Specification

## 1. What a Role is

A Role is a list of rules. Each rule says what may be done, and to what:

- "Read the staging configs."
- "Run the monitoring playbooks on the staging configs."

A Role grants nothing until it is bound to subjects.

```yaml
apiVersion: mission-control.flanksource.com/v1
kind: Role
metadata:
  name: staging-operator
  namespace: default
spec:
  rules:
    - name: read-staging
      action: read
      resource:
        scopeRef: staging-configs
    - name: run-monitoring
      action: playbook:run
      resource:
        scopeRef: monitoring-playbooks
      target:
        scopeRef: staging-configs
```

| Field               | Required              | Meaning                                                                                      |
| ------------------- | --------------------- | -------------------------------------------------------------------------------------------- |
| `name`              | Yes                   | Unique within the Role.                                                                      |
| `action`            | Yes                   | What may be done (Section 2).                                                                |
| `resource.scopeRef` | Yes                   | The Scope of resources the action is performed on, e.g. the playbooks to run.                |
| `target.scopeRef`   | Depends on the action | The Scope of resources the action is performed against, e.g. the configs a playbook runs on. |
| `deny`              | No                    | Deny the action instead of allowing it.                                                      |

A Scope is a named set of resources (see `scopes.md`). A rule references exactly one Scope per input, by name. A Role MUST have a namespace, and `scopeRef` only names Scopes in it (`overview.md`, "Namespaces"). The resources a Scope selects can be in any namespace.

A Role can't be named after a built-in role: `admin`, `everyone`, `guest`, `viewer`, `editor`, `commander`, `responder` or `agent`. Such a Role is rejected (`overview.md`, "Rejected or not in effect"). The built-in roles are described in Section 7. In a RoleBinding, `role` always names a Role and `subjects.roles` always names a built-in role (`rolebindings.md`, Section 2.3), so `role: viewer` must never read as the built-in `viewer`.

### 1.1 Why one Scope per input

Each input takes a single Scope. To cover more resources, add targets to the Scope, or create a new Scope that selects all of them. A list of Scopes would add nothing a Scope can't already do.

A single Scope also prevents accidental grants. Suppose a rule could list several Scopes:

```yaml
# Not supported
- name: run-playbooks
  action: playbook:run
  resource:
    scopeRefs: [echo-playbook, restart-playbook]
  target:
    scopeRefs: [staging-configs, production-configs]
```

The intent might be "`echo` on staging, `restart-pod` on production". But the rule allows both playbooks on both sets of configs, so it also allows `echo` on production and `restart-pod` on staging. With a single Scope per input, each pairing is its own rule (Section 4.2):

```yaml
- name: run-echo-on-staging
  action: playbook:run
  resource:
    scopeRef: echo-playbook
  target:
    scopeRef: staging-configs
- name: run-restart-on-production
  action: playbook:run
  resource:
    scopeRef: restart-playbook
  target:
    scopeRef: production-configs
```

## 2. Actions

Every action has a contract, defined in code: the resource types it accepts, and whether it takes a target and of which types. An action without a contract can't be used in a rule. The contracts are:

| Action                                                | Resource                                               | Target                             | Can deny |
| ----------------------------------------------------- | ------------------------------------------------------ | ---------------------------------- | -------- |
| `read`                                                | Config, Component, Check, Canary, Playbook, Connection | None                               | Not yet  |
| `create`, `update`, `delete`                          | Config, Component, Canary, Playbook, Connection        | None                               | Yes      |
| `playbook:run`, `playbook:approve`, `playbook:cancel` | Playbook                                               | Optional: Config, Component, Check | Yes      |
| `mcp:run`                                             | Playbook                                               | None                               | Yes      |
| `invoke:<plugin>:<operation>`                         | Config                                                 | None                               | Yes      |

For `playbook:run`, `target` is optional. A rule without it matches only runs with no target. A rule with `target.scopeRef` matches only runs on Configs, Components or Checks in that Scope (Section 4.1).

- Actions are matched by exact name. Patterns such as `playbook:*` or `invoke:kubernetes-logs:*` aren't supported, for two reasons:
  - A pattern can cover actions that accept different resources. `*` covers both `read` (configs) and `playbook:run` (playbooks), and no single Scope fits both.
  - A pattern grants actions added later. If the `kubernetes-logs` plugin ships a new `exec-shell` operation, `invoke:kubernetes-logs:*` would grant it without anyone reviewing the Role.
- A plugin action isn't checked against the installed plugins: plugins are installed and upgraded often, and a Role mustn't break when one is. A rule on an operation no plugin declares matches nothing until one does.
- `create`, `update` and `delete` are only checked on all resources of a type, so their Scope MUST consist of whole-type targets only (`scopes.md`, Section 5.2).
- Deny rules on `read` are rejected for now, because they couldn't be enforced on listings: Mission Control's own users' database listings aren't filtered by row unless they're guests (`overview.md`, "Default access"). A deny on reading production configs would still let an editor list them.

An operation may make more than one check. For example, running a playbook on a config also checks `read` on that config. Section 4.3 lists every check each operation makes; a rule never grants the other checks implicitly.

### 2.1 Why one action per rule

A rule has exactly one action, because the action decides what the rest of the rule means. Its contract fixes the resource types the `resource` Scope may select, whether the rule takes a `target` and of which types, what its Scopes must meet (Section 3), whether it can be a deny, and how it's enforced. A rule with several actions would need one Scope to meet several contracts at once. One action per rule also lets an invalid rule name the action that's wrong. To grant several actions on the same Scope, write a rule for each.

## 3. Which Scopes a rule accepts

Every type a Scope selects MUST be accepted by the input it fills (Section 2). Otherwise the rule is invalid, and so is the Role: it's `Ready=False` and none of its rules apply (Section 6). A rule never uses part of a Scope.

Whether a Scope fits depends on the Scope, which can change after the Role is written, so a Role whose Scope doesn't fit is stored `Ready=False`, not rejected (`overview.md`, "Rejected or not in effect"). The same holds for every other requirement a rule's Scopes must meet (Sections 2 and 3.1). What a rule says on its own is checked when it's written, and a Role that fails it is rejected: an unknown action or a pattern, a deny on an action that can't be denied, a `target` on an action that takes none, a rule name used twice, or a reserved Role name.

Given these Scopes:

```yaml
kind: Scope
metadata:
  name: monitoring-playbooks
spec:
  targets:
    - playbook:
        namespace: monitoring
---
kind: Scope
metadata:
  name: staging
spec:
  targets:
    - config:
        tagSelector: env=staging
    - component:
        namespace: staging
---
kind: Scope
metadata:
  name: staging-dashboards
spec:
  targets:
    - view:
        namespace: staging
```

Good: playbooks as the resource, configs and components as the target.

```yaml
- name: run-monitoring
  action: playbook:run
  resource:
    scopeRef: monitoring-playbooks
  target:
    scopeRef: staging
```

Bad: `playbook:run` runs playbooks, but `staging` selects configs and components.

```yaml
- name: run-staging
  action: playbook:run
  resource:
    scopeRef: staging
```

Bad: a playbook can't run on a view.

```yaml
- name: run-on-dashboards
  action: playbook:run
  resource:
    scopeRef: monitoring-playbooks
  target:
    scopeRef: staging-dashboards
```

### 3.1 The `read` action

A rule with `action: read` is checked in two places:

1. **On one resource**, e.g. opening a config.
2. **On listings**, e.g. listing configs through the database API, where the rows are filtered to the ones the subject may read.

Both MUST allow the same resources at every moment (`scopes.md`, Section 7.1): a resource a subject can open appears in their listings, and a resource in their listings can be opened. How Mission Control keeps the two in step is the design's business (`design/materialised-membership.md`).

A `read` rule accepts any Scope whose membership is decided by the resource alone (`scopes.md`, Section 4.3), which every Scope is, with one exception: connections aren't filtered by row, so a `read` rule's Scope MUST select connections with a whole-type target only.

Listings are only filtered while row-level security is enabled. It's turned on or off when Mission Control starts, from the `rls.enable` property, so changing the property takes effect on restart. So a `read` rule whose Scope has a target that isn't a whole-type target (`scopes.md`, Section 5.2) needs it: while row-level security is off, a Role with such a rule is `Ready=False` with reason `RowLevelSecurityRequired`, and none of its rules apply, like any invalid Role (Section 6). It becomes valid when row-level security is enabled, without being re-applied. A rule whose Scope consists of whole-type targets only doesn't need it: opening any resource and listing all of them allow the same resources.

Whether a subject may list a type at all, and what a listing returns for a subject whose grants cover only part of it, is specified in `collection-access.md`. Subjects without built-in access (`overview.md`, "Default access") list only what their `read` grants select, and a listing of a type none of their grants covers is refused with `403 Forbidden`, whether or not row-level security is on.

## 4. Matching

A rule matches an operation when all three hold:

1. The operation's action is the rule's `action`.
2. The operation's resource is in the rule's `resource` Scope.
3. The operation's target matches the rule's `target` (Section 4.1).

For example, this rule matches running `restart-pod` on a staging config, because `restart-pod` is in `monitoring-playbooks` and the config is in `staging-configs`:

```yaml
- name: run-monitoring
  action: playbook:run
  resource:
    scopeRef: monitoring-playbooks
  target:
    scopeRef: staging-configs
```

It doesn't match running `restart-pod` on a production config, or running any playbook outside `monitoring-playbooks`.

### 4.1 Targets

| Rule `target` | Operation's target         | Matches |
| ------------- | -------------------------- | ------- |
| Omitted       | None                       | Yes     |
| Omitted       | Present                    | No      |
| Set           | In the Scope               | Yes     |
| Set           | None, or outside the Scope | No      |

So a rule without a target only covers operations without one. To cover running on any config, use a Scope of all configs as the target. The same holds for deny rules.

### 4.2 Resources and targets stay paired

A rule's resources and targets are only combined within that rule:

```yaml
rules:
  - name: echo-on-staging
    action: playbook:run
    resource:
      scopeRef: echo-playbook
    target:
      scopeRef: staging-configs
  - name: restart-on-production
    action: playbook:run
    resource:
      scopeRef: restart-playbook
    target:
      scopeRef: production-configs
```

This allows `echo` on staging and `restart` on production, but not `echo` on production.

### 4.3 What an operation must provide

A rule is matched against the actual resource and target of an operation:

- The operation MUST name its resource, and its target if it has one. A missing or unknown target is never treated as "no target".
- An operation that doesn't fit the action, such as a target of the wrong type, matches no allow rule and every deny rule.
- An entry point may require an extra permission, e.g. `mcp:run` for playbooks run through MCP. It never replaces the check of the operation itself.

A check carries only the resources its action's contract can match (Section 2). An action without a target carries the resource alone, even when the operation it gates has one. So each operation makes these checks:

| Operation                             | Check                                       | Resource        | Target                            |
| ------------------------------------- | ------------------------------------------- | --------------- | --------------------------------- |
| Run playbook P with no resource       | `playbook:run`                              | P               | None                              |
| Run playbook P on resource X          | `playbook:run`, then `read`                 | P, then X       | X, then none                      |
| Run playbook P through MCP            | `mcp:run`, then the checks of the run       | P               | None                              |
| Approve or cancel run R               | `playbook:approve` or `playbook:cancel`     | R's playbook    | R's resource, if the run had one  |
| Invoke a plugin operation on config C | `invoke:<plugin>:<operation>`, then `read`  | C, then C       | None                              |
| Open resource X                       | `read`                                      | X               | None                              |

### 4.4 Open question: deny rules without a target

**TODO:** This Role looks like it stops everyone from running `restart-pod`, but it doesn't:

```yaml
apiVersion: mission-control.flanksource.com/v1
kind: Role
metadata:
  name: no-restart
  namespace: default
spec:
  description: Only on-call may restart pods
  rules:
    - name: deny-restart
      action: playbook:run
      deny: true
      resource:
        scopeRef: restart-pod-playbook
```

The rule has no target, so it only covers runs of `restart-pod` without a resource (Section 4.1). Anyone otherwise allowed to run `restart-pod` on a pod config can still do it. Once a run must fit its playbook (`specs/playbooks.md`), `restart-pod` can't run without a resource at all, and this rule blocks nothing. Nothing tells the author.

Find a design that removes this trap. What's been considered so far:

- **Make a deny rule without a target cover every target.** Rejected: allow and deny rules would read the same field differently, which users would have to know in advance.
- **Add `none` and `any` values to `target`.** Rejected: `target: {none: true}` is confusing to read.
- **Make `target` mandatory on `playbook:run`.** A ban then names every target, e.g. a Scope selecting every config, component and check. But a rule can no longer cover a playbook that runs on its own, like `create-namespace`. A separate action for those playbooks, e.g. `playbook:run-standalone`, would fill that gap; `playbook:approve` and `playbook:cancel` would need the same split.
- **Ask two questions per run**, "may this person run the playbook?" and "may they run it on this resource?". Rejected: a deny rule without a target would ban the playbook everywhere, which is again something users would have to know in advance.

## 5. Allow and deny

Across all the rules that apply to a subject, from every Role they're bound to, the same rule decides:

- An operation is allowed when an allow rule matches and no deny rule does.
- A deny always wins, over built-in access too: a deny on `delete` stops an editor even though the built-in `editor` role allows it. Only admins are exempt.
- Order doesn't matter, and it doesn't matter which Role a rule comes from.
- Roles only add access. A Role can't remove access another Role grants, except with a deny.
- Row filters follow the same rule: a subject's rows are the ones any of their `read` rules allows (Section 3.1).

None of this applies to admins: no rule, allow or deny, applies to them (`overview.md`, "Default access").

## 6. How changes take effect

A Role is validated against what it references: its rules, and the Scopes they name. It's never validated against the bindings that reference it. A Role change goes through even when a binding's constraint can't narrow the new rule: that rule doesn't apply through the binding, the rest of the Role does, and the binding reports it (`rolebindings.md`, Sections 3.2 and 4). The Role's status lists the bindings reporting one of its rules. That's information, not validation: it doesn't affect whether the Role is valid.

A Role that's wrong on its own is rejected (Section 3). Otherwise it's stored, and takes effect only when all its rules are valid. One invalid rule makes the whole Role invalid: it's `Ready=False` with the reason, and none of its rules apply, allow or deny. There is no previous version to fall back to; the Role is whatever was last written.

A Role becomes invalid when it's written with an invalid rule, when a Scope it references changes into one a rule can't accept, is deleted, or becomes invalid itself (`scopes.md`, Section 7), or when row-level security is turned off while a rule needs it (Section 3.1). It becomes valid again, and its rules apply again, as soon as the cause is gone, without being re-applied.

### 6.1 Keep Roles small

Because one broken rule silences the whole Role, a rule is only as reliable as the rules beside it. Prefer several small Roles over one large one, and bind them together:

- Put a deny rule in its own Role, with Scopes nothing else edits. A deny that shares a Role with an allow rule stops denying whenever that allow rule breaks.
- Group rules by the Scopes they share, so a Scope change invalidates one Role, not every Role.
- After adding a rule to a Role that's bound with a constraint, check the Role's status for bindings the rule doesn't apply through (Section 6).

A subject bound to several Roles holds the union of the valid ones (Section 5), so splitting a Role changes nothing while all of them are valid.

## 7. Built-in roles

Besides the Roles written as resources, Mission Control has built-in roles, defined in code. A person is given a built-in role when they're invited, by their identity provider's role mapping, or by an admin. RoleBindings can select every user, every guest or every agent as subjects, with `roles` (`rolebindings.md`, Section 2.3), but no Role can be named after a built-in role (Section 1).

The built-in roles answer two separate questions:

- **How much can you see?** Members are the organisation's own users and see every resource. Guests are outsiders and only see the resources shared with them.
- **What can you do?** Viewers can only read. Editors can also change things.

| Role        | Who it's for                                                        | Can do                                                                                                                                                                                                                     |
| ----------- | ------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `everyone`  | Every Mission Control user                                          | Nothing on its own, apart from what's granted to it. It's denied the Kratos database                                                                                                                                       |
| `admin`     | Members who administer Mission Control                              | Everything. No rule applies to them, deny rules included (`overview.md`, "Default access")                                                                                                                                 |
| `viewer`    | Members who only read                                               | `read` on the catalog, topology, canaries, playbooks, views, people, applications and the public database tables, as whole types, with listings unfiltered                                                                 |
| `editor`    | Members who also change things                                      | What a viewer can, plus `create`, `read`, `update` and `delete` on canaries, the catalog, topology, playbooks, the Kubernetes proxy, notifications, applications and connections, and `read` on connection details         |
| `guest`     | Outsiders, e.g. contractors, customers or another team's users      | The built-in read access every user has, but their listings are always filtered by row, to what Permissions and RoleBindings grant them (Section 7.3)                                                                      |
| `agent`     | Mission Control agents pushing data from other clusters             | `read` on playbooks and the public database tables; `create`, `read` and `update` on agent pushes; `create` and `update` on topology                                                                                       |
| `commander` | **TODO:** not defined yet                                           | **TODO**                                                                                                                                                                                                                   |
| `responder` | **TODO:** not defined yet                                           | **TODO**                                                                                                                                                                                                                   |

### 7.1 Everyone

`everyone` is every Mission Control user: every person, whatever their built-in role, agents included. A user is a member of `everyone` directly, so no role needs to inherit it. Users of an external identity provider aren't Mission Control users, and aren't members (`external-identity-providers.md`, Section 6).

### 7.2 Members

`viewer` is the base member role. `editor`, `commander` and `responder` inherit it, so they see everything a viewer sees, and anything granted to `viewer` reaches them too. Members' listings are never filtered by row: a member sees every resource of a type they can read.

### 7.3 Guests

A guest is the restricted role. Guests get past the same whole-type checks as viewers for `read`, the built-in access every user has (`overview.md`, "Default access"). But their listings are always filtered by row, to the resources their Permissions and RoleBindings grant (Section 3.1). So a guest sees only what's been shared with them.

`guest` doesn't inherit `viewer`. A guest isn't reached by what's granted to `viewer` or to any other member role.

### 7.4 Open questions

- **TODO:** A person with no built-in role. Today they get the built-in access every user has, unfiltered, so in practice they're a viewer. Decide whether that's intended, or whether they should get nothing until they're given a role.
- **TODO:** Reads granted to `viewer`. Section 7.3 says they don't reach guests, but the code currently lets them reach every user, guests and agents included, on whole-type checks. Confirm guests are excluded, and fix the code.
- **TODO:** Guest visibility is only defined for listings with row-level security on. Decide (1) whether a guest opening one resource directly, e.g. a config by id, is covered by the built-in whole-type `read` or checked against their grants only, and (2) what a guest's listing returns while row-level security is off: Section 3.1 says listings are only filtered while it's on, Section 7.3 says guests' listings are always filtered.
