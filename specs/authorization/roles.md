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

A Scope is a named set of resources (see `scopes.md`). A rule references exactly one Scope per input, by name. A Role MUST have a namespace, and `scopeRef` only names Scopes in it (`overview.md`, "Namespaces"). The resources a Scope selects can be in any namespace.

Mission Control ships a few Roles, e.g. `viewer` and `admin` (`overview.md`, "Shipped Roles"). They're Roles like any other.

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

| Action                                                | Resource                                               | Target                             |
| ----------------------------------------------------- | ------------------------------------------------------ | ---------------------------------- |
| `read`                                                | Every type but Property                                | None                               |
| `create`, `update`, `delete`                          | Every type but Check, Event and Job                    | None                               |
| `playbook:run`, `playbook:approve`, `playbook:cancel` | Playbook                                               | Optional: Config, Component, Check |
| `mcp:run`                                             | Playbook                                               | None                               |
| `invoke:<plugin>:<operation>`                         | Config                                                 | None                               |
| `kubernetes:proxy`                                    | Config                                                 | None                               |

The types are those of `scopes.md`, Section 3.1. Checks, events and jobs are only written by Mission Control itself. Properties aren't read through a rule (Section 3.1). `kubernetes:proxy` allows calling the Kubernetes API of a cluster config, e.g. one of type `Kubernetes::Cluster`, through Mission Control's proxy.

For `playbook:run`, `target` is optional. A rule without it matches only runs with no target. A rule with `target.scopeRef` matches only runs on Configs, Components or Checks in that Scope (Section 4.1).

- Actions are matched by exact name. Patterns such as `playbook:*` or `invoke:kubernetes-logs:*` aren't supported, for two reasons:
  - A pattern can cover actions that accept different resources. `*` covers both `read` (configs) and `playbook:run` (playbooks), and no single Scope fits both.
  - A pattern grants actions added later. If the `kubernetes-logs` plugin ships a new `exec-shell` operation, `invoke:kubernetes-logs:*` would grant it without anyone reviewing the Role.
- A plugin action isn't checked against the installed plugins: plugins are installed and upgraded often, and a Role mustn't break when one is. A rule on an operation no plugin declares matches nothing until one does.
- `create`, `update` and `delete` are only checked on all resources of a type, so their Scope MUST consist of whole-type targets only (`scopes.md`, Section 5.2).

An operation may make more than one check. For example, running a playbook on a config also checks `read` on that config. Section 4.3 lists every check each operation makes; a rule never grants the other checks implicitly.

### 2.1 Why one action per rule

A rule has exactly one action, because the action decides what the rest of the rule means. Its contract fixes the resource types the `resource` Scope may select, whether the rule takes a `target` and of which types, what its Scopes must meet (Section 3), and how it's enforced. A rule with several actions would need one Scope to meet several contracts at once. One action per rule also lets an invalid rule name the action that's wrong. To grant several actions on the same Scope, write a rule for each.

## 3. Which Scopes a rule accepts

Every type a Scope selects MUST be accepted by the input it fills (Section 2). Otherwise the rule is invalid, and so is the Role: it's `Ready=False` and none of its rules apply (Section 6). A rule never uses part of a Scope.

Whether a Scope fits depends on the Scope, which can change after the Role is written, so a Role whose Scope doesn't fit is stored `Ready=False`, not rejected (`overview.md`, "Rejected or not in effect"). The same holds for every other requirement a rule's Scopes must meet (Sections 2 and 3.1). What a rule says on its own is checked when it's written, and a Role that fails it is rejected: an unknown action or a pattern, a `target` on an action that takes none, or a rule name used twice.

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

Records follow the resource they belong to (`scopes.md`, Section 3.2): a playbook run is listed and opened exactly when its playbook can be read. A database table that's neither a type nor a record of one isn't served through the database API.

Properties are the one thing read without a rule: anyone may read them, signed in or not. They configure Mission Control's behaviour, e.g. which pages are enabled, and MUST NOT hold anything confidential. Writing them takes `create`, `update` or `delete` on `property` like any other type. _Why:_ every page needs them before it can decide what to show, a subject with no grants included.

Whether a subject may list a type at all, and what a listing returns for a subject whose grants cover only part of it, is specified in `collection-access.md`. Every subject lists only what their `read` grants select, and a listing of a type none of their grants covers is refused with `403 Forbidden`, whether or not row-level security is on.

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

So a rule without a target only covers operations without one. To cover running on any config, use a Scope of all configs as the target.

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
- An operation that doesn't fit the action, such as a target of the wrong type, matches no rule.
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

## 5. Combining rules

Across all the rules that apply to a subject, from every Role they're bound to:

- An operation is allowed when any rule matches it, and refused otherwise.
- Order doesn't matter, and it doesn't matter which Role a rule comes from.
- Roles only add access. No Role can remove access another Role grants: rules only allow (`overview.md`, "Access").
- Row filters follow the same rule: a subject's rows are the ones any of their `read` rules allows (Section 3.1).

## 6. How changes take effect

A Role is validated against what it references: its rules, and the Scopes they name. It's never validated against the bindings that reference it. A Role change goes through even when a binding's constraint can't narrow the new rule: that rule doesn't apply through the binding, the rest of the Role does, and the binding reports it (`rolebindings.md`, Sections 3.2 and 4). The Role's status lists the bindings reporting one of its rules. That's information, not validation: it doesn't affect whether the Role is valid.

A Role that's wrong on its own is rejected (Section 3). Otherwise it's stored, and takes effect only when all its rules are valid. One invalid rule makes the whole Role invalid: it's `Ready=False` with the reason, and none of its rules apply. There is no previous version to fall back to; the Role is whatever was last written.

A Role becomes invalid when it's written with an invalid rule, when a Scope it references changes into one a rule can't accept, is deleted, or becomes invalid itself (`scopes.md`, Section 7), or when row-level security is turned off while a rule needs it (Section 3.1). It becomes valid again, and its rules apply again, as soon as the cause is gone, without being re-applied.

### 6.1 Keep Roles small

Because one broken rule silences the whole Role, a rule is only as reliable as the rules beside it. Prefer several small Roles over one large one, and bind them together:

- Group rules by the Scopes they share, so a Scope change invalidates one Role, not every Role.
- After adding a rule to a Role that's bound with a constraint, check the Role's status for bindings the rule doesn't apply through (Section 6).

A subject bound to several Roles holds the union of the valid ones (Section 5), so splitting a Role changes nothing while all of them are valid.
