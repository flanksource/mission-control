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
| `resource.scopeRef` | Depends on the action | The Scope of resources the action is performed on, e.g. the playbooks to run. Omitted for the few actions that take no resource (Section 2.3). |
| `target.scopeRef`   | Depends on the action | The Scope of resources the action is performed against, e.g. the configs a playbook runs on. |

A Scope is a named set of resources (see `scopes.md`). A rule references exactly one Scope per input. A Role MUST have a namespace. A bare `scopeRef` names a Scope in the Role's namespace, never the binding's, and `namespace/name` names one in any namespace (`overview.md`, "References"). The resources a Scope selects can be in any namespace.

_Why:_ a Role grants the same rules wherever it's bound, and its Scopes don't have to be copied into each binding's namespace.

A Role has at least one rule, and at most 200; the Roles Mission Control ships aren't limited, since `plugin-admin` has a rule per plugin operation. _Why 200:_ the number is arbitrary, and there's no technical reason for it. It's high because the shipped `admin` Role has a rule for every action on every type, 77 in all (`overview.md`, "Shipped Roles"), and a Role written by hand must be able to grant as much.

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
| `read`                                                | Every type                                             | None                               |
| `create`, `update`, `delete`                          | Every type but Check, Person and View                  | None                               |
| `playbook:run`, `playbook:approve`, `playbook:cancel` | Playbook                                               | Optional: Config, Component, Check |
| `mcp:run`                                             | Playbook                                               | None                               |
| `invoke:<plugin>:<operation>`                         | Config                                                 | None                               |
| `connection:use`                                      | Connection                                             | None                               |
| `person:manage`                                       | None                                                   | None                               |
| `access:review`                                       | None                                                   | None                               |
| `mcp:use`                                             | None                                                   | None                               |
| `property:manage`                                     | None                                                   | None                               |
| `event:read`                                          | None                                                   | None                               |
| `job:read`                                            | None                                                   | None                               |
| `kubernetes-proxy:read`, `kubernetes-proxy:write`     | None                                                   | None                               |

The types are those of `scopes.md`, Section 3.1. Views are only read (Section 3.2): no other action accepts them. Checks are only written by Mission Control itself. Properties, the event queue, job history and the Kubernetes proxy aren't types: they're reached through actions without a resource (Section 2.3). `connection:use` allows acting with a connection's credentials (Section 2.2).

`person:manage` allows everything about accounts: inviting someone to Mission Control, disabling and re-enabling them, changing their properties, name or email, and deleting them. People aren't created, changed or deleted any other way, so `create`, `update` and `delete` don't apply to them. _Why:_ inviting someone and managing their account are what an admin actually does, and a named action says so. A rule allowing `create` on people would leave its reader asking where people are created.

`person:manage` is as privileged as `admin`, and is only for subjects trusted as admins. An invite that names teams adds the person to them when it's accepted (`rolebindings.md`, Section 2.2), so it grants whatever the teams' bindings grant. Changing a person's email changes which `people` subjects select them (`rolebindings.md`, Section 2.1), so it moves bindings from one person to another. _Why one action:_ inviting, managing and deleting accounts are equally privileged, so splitting them would only suggest that one is safer to grant than the others.

For `playbook:run`, `target` is optional. A rule without it matches only runs with no target. A rule with `target.scopeRef` matches only runs on Configs, Components or Checks in that Scope (Section 4.1).

- Actions are matched by exact name. Patterns such as `playbook:*` or `invoke:kubernetes-logs:*` aren't supported, for two reasons:
  - A pattern can cover actions that accept different resources. `*` covers both `read` (configs) and `playbook:run` (playbooks), and no single Scope fits both.
  - A pattern grants actions added later. If the `kubernetes-logs` plugin ships a new `exec-shell` operation, `invoke:kubernetes-logs:*` would grant it without anyone reviewing the Role.
- A plugin action isn't checked against the installed plugins: plugins are installed and upgraded often, and a Role mustn't break when one is. A rule on an operation no plugin declares matches nothing until one does.
- `create`, `update` and `delete` MUST pick whole types (`scopes.md`, Section 3.1).

An operation may make more than one check. For example, running a playbook on a config also checks `read` on that config. Section 4.3 lists every check each operation makes; a rule never grants the other checks implicitly.

### 2.1 Why one action per rule

A rule has exactly one action, because the action decides what the rest of the rule means. Its contract fixes the resource types the `resource` Scope may select, whether the rule takes a `target` and of which types, what its Scopes must meet (Section 3), and how it's enforced. A rule with several actions would need one Scope to meet several contracts at once. One action per rule also lets an invalid rule name the action that's wrong. To grant several actions on the same Scope, write a rule for each.

### 2.2 Reading and using a connection

A connection is read and used separately:

| Operation                                                                                     | Action           |
| --------------------------------------------------------------------------------------------- | ---------------- |
| List or open a connection: its name, type and settings, and which secret it references        | `read`           |
| Act with its credentials: a notification sending through it, a playbook or plugin calling it | `connection:use` |
| Test it                                                                                       | `connection:use` |
| Create, change or delete it                                                                   | `create`, `update`, `delete` |

- `read` MUST NOT return a connection's credentials, in plain text or decrypted from a secret. They're masked wherever a connection is shown.
- Mission Control MUST check `connection:use` wherever it fills in a connection's credentials, as the subject doing the work (`identities.md`, Section 2). Looking a connection up by name to record which one is meant, without its credentials, checks nothing.
- The check is made on the connection being used, found exactly as it's found to fill in its credentials. `connection:use` on it covers every connection its settings name, e.g. an HTTP connection's AWS signing connection: their credentials are filled in for it without a check of their own. _Why:_ a connection is granted as a whole. Whoever may use it may use it as it's configured, whatever it relies on, so a grant on it never depends on how it's built. Whoever may change its settings (`update` on `connection`) decides what it relies on.
- A `connection:use` rule's Scope may pick some connections, unlike a `read` rule's (`scopes.md`, Section 3.1): it's only checked on the connection being used, never on a listing.

_Why two actions:_ seeing that a connection exists and acting with its credentials are different risks. With one action, letting someone see the connections a notification can use would let anything bound the same way send with them. _Why testing is a use:_ a test makes an authenticated call to the other system, and its result can show what the masked connection doesn't, e.g. whether the credentials work.

### 2.3 Actions without a resource

A few actions aren't performed on a resource a Scope could select. They act on a type as a whole, or on Mission Control itself. They take no `resource`, and a rule that sets one is rejected. They're exactly these:

| Action            | Allows                                                                                         |
| ----------------- | ---------------------------------------------------------------------------------------------- |
| `person:manage`   | Inviting, disabling, re-enabling and deleting accounts, and changing their properties, name or email |
| `access:review`   | Asking what any subject may do: subject access reviews and searches (`/rbac/subject-access-reviews`, `/rbac/subject-access-search`) |
| `mcp:use`         | Using Mission Control's MCP server at all. Each MCP tool still checks what it touches (Section 4.3) |
| `property:manage` | Reading private properties and creating, changing and deleting any property. Reading public properties needs no rule (Section 3.1) |
| `event:read`      | Reading Mission Control's event queue, and the recent events playbooks and notifications have handled |
| `job:read`        | Reading all of Mission Control's job history                                                   |
| `kubernetes-proxy:read`  | Kubernetes API requests through Mission Control's proxy, as the Kubernetes user `mission-control-reader` |
| `kubernetes-proxy:write` | Kubernetes API requests through Mission Control's proxy, as the Kubernetes user `mission-control-writer` |

```yaml
rules:
  - name: manage-people
    action: person:manage
  - name: use-mcp
    action: mcp:use
  - name: read-jobs
    action: job:read
```

- `access:review` only answers whether a subject may perform an action. It grants reading nothing: a search only returns resources the reviewer may read themselves, so a review never names a resource the reviewer can't see. Reviewing access across a type takes `read` on that type too.
- A review is checked as the operation would check the subject: on the resource and, for an action that takes one, its target, or on the whole type for `create`, `update` and `delete`, and for reading connections (Section 2.2). It also says whether the subject passes what the operation checks besides the action, e.g. `read` on the target of a run, an approval or a cancellation: the operation is refused when it doesn't. A review MAY explain itself: it lists every rule the subject holds for the action, whether each matches, and the teams it's held through, read from the same moment of the policy as its answer. It names a RoleBinding, Role, rule or team only to a reviewer who may `read` that type.
  _Why one moment:_ an explanation read separately from its answer can contradict it while the policy changes.
- `property:manage` is one action for reading private properties and every property write. A property changes what Mission Control does for everyone, e.g. which pages are enabled, so creating, changing and deleting one are equally privileged.
- `event:read` covers the event queue (`event_queue`) and the recent events kept in memory by playbooks and notifications. Events are only written by Mission Control itself, so no action writes them.
- `job:read` covers all job history, e.g. a cleanup job's runs and a scraper's runs, whether or not a job ran for a resource. A subject with it sees all of it, and a subject without it sees none, whatever resources they may read (`collection-access.md`, Section 3.1). A resource's latest job status shown in its own row stays visible with the resource (Section 3.1).
- `job:read` is as privileged as `admin`, like `person:manage`, and is only for subjects trusted as admins. A job's details and errors mention the resources it worked on, e.g. configs a scraper found or the playbook a run was refused for, which the holder may not otherwise be able to read. _Why not per resource:_ one job touches many resources, and its details aren't split by resource, so a row can't be shown only to those who may read everything it mentions.

- The Kubernetes proxy reaches only the cluster Mission Control runs in. It authenticates as Mission Control's own service account and impersonates `mission-control-writer` when the subject has `kubernetes-proxy:write`, otherwise `mission-control-reader` when it has `kubernetes-proxy:read`, and otherwise the request is refused. _Why write implies read:_ the writer user may read everything the reader may, so a subject never needs both.
- What each user may do in the cluster is decided by the cluster's RBAC, which Mission Control's installation sets up.
- The proxy is checked on every request, as the subject making it, so a token's limit and a deny binding apply to the next request.

- Every other action takes a resource, and a rule that omits it is rejected. `read`, `create`, `update`, `delete` and every other generic action always take one: no rule grants them on everything.
- An action without a resource is named `<type>:<verb>`, after what it acts on, so the rule still says what it's about without a Scope.
- A RoleBinding constraint has nothing to narrow on such a rule, so the rule never applies through a binding with a constraint, and the binding reports it (`rolebindings.md`, Section 3.2).

_Why:_ the person being invited doesn't exist yet, and the MCP server isn't a resource, so a Scope would have nothing to select and would only repeat the action's name. The Kubernetes proxy reaches one cluster, so a Scope could only select all of it. Properties, events and job history are Mission Control's own settings and internals: nobody grants some of them and not others, so a Scope could only ever select all of them. Keeping the list closed and named means every other rule still says what it's on.

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

A `read` rule accepts some or all of a type wherever its type allows it (`scopes.md`, Section 3.1). For connections, that's all of them only.

Listings are filtered by row-level security, which is always on: there's no setting to turn it off. So a `read` rule whose Scope picks some of a type (`scopes.md`, Section 3.1) filters listings to what it selects. A rule whose Scope consists of whole-type targets only doesn't need row filters: opening any resource and listing all of them allow the same resources. A binding's constraint can still narrow it, and then its listings are filtered by the constraint's Scope (`rolebindings.md`, Section 3.2).

**Records.** Some data belongs to a resource rather than being one: a config's changes and analysis, a playbook's runs, a notification's send history and a check's statuses. A record isn't a type. It's listed and opened exactly when every resource it belongs to can be read:

- A playbook run belongs to its playbook, and to the config or check it ran on, if any.
- A run's steps, approvals and the data its agent reports belong to the run, so they follow the run.
- A relationship between two configs belongs to both.
- An artifact, e.g. a file a run's step or a check produced, belongs to what produced it: a run's step, a check, a config change or a scraper. So an artifact from a playbook run is visible exactly when the run is, whoever started the run.

Job history isn't a record, even when a job ran for a resource, e.g. a scraper's runs: it's read whole with `job:read`, or not at all (Section 2.3). A resource's latest job status, where it's shown in the resource's own row, e.g. a canary's in `canaries_with_status`, is part of the resource and is visible with it.

**TODO:** runs on a component. Today they follow their playbook alone, so anyone who can read the playbook sees them.

_Why:_ who may see a run or a change is never a separate question from who may see what it's about. A run on a config is about the config as much as the playbook, so a subject who can't read the config can't read the run, its steps or their results.

Lookup views derived from a type, e.g. `config_types` or `config_tags`, which list the distinct values the UI's filters offer, are readable by anyone with a `read` grant on that type, and MUST only show values from rows the subject can read. A database table that's neither a type, a record of one, nor a lookup view derived from one isn't served through the database API.

Properties whose names start with `public.` MUST be readable by any signed-in subject without a rule, and MUST NOT hold anything confidential. All other properties are private. Private property reads and all property writes MUST require `property:manage` (Section 2.3). The properties table MUST NOT be exposed through PostgREST; only the read-only `feature_flags` view of public properties is. Stored properties are managed through `/property`.

_Why:_ every page needs public settings before it can decide what to show. The name explicitly makes a property public without registering it in code; backend configuration stays private by default.

Whether a subject may list a type at all, and what a listing returns for a subject whose grants cover only part of it, is specified in `collection-access.md`. Every subject lists only what their `read` grants select, and a listing of a type none of their grants covers is refused with `403 Forbidden`.

### 3.2 Views

A `read` rule on a Scope of views lets its subjects open the views it selects, some or all of them like any other type (`scopes.md`, Section 3.1). A view is granted whole: a subject who may read a view sees every row and every panel of it, and one who may not sees none of them. There's no grant of some rows or some panels of a view.

```yaml
kind: Scope
metadata:
  name: staging-dashboards
  namespace: default
spec:
  targets:
    - view:
        namespace: staging
---
rules:
  - name: read-staging-dashboards
    action: read
    resource:
      scopeRef: staging-dashboards
```

- A view's rows and panels are read with the view, not through the resources its queries read: a subject who may read a view sees every row and panel stored for it, whatever configs they may read, and one who may read every config but not the view sees none.
- Listing views lists the ones the subject may read, like any type (Section 3.1).
- Every way of reading a view or its rows makes the same check on that view: the UI and the views API, the dashboard, the database API (`views`, `view_panels` and each view's generated table), MCP, exports, an application's view sections, and a playbook's report action. The report action is checked as its playbook, like every action of a run (`identities.md`, Section 2).
- A view's column definitions, without its rows, are readable by agents without a rule (`identities.md`, Section 5).
- **TODO:** which subject a view's queries are run as when it's refreshed, and so which connections and resources a refresh may use, isn't specified here.

_Why whole views:_ which views a subject may open is an ordinary resource question, answered like it is for every other type. Which rows of a view they may see isn't: a view's rows are arbitrary data from several sources, e.g. configs, SQL and Prometheus, computed once and shared by every reader, so they can't simply follow the grants of the resources they came from (flanksource/mission-control#3539). Granting some rows will add a condition to view targets; a target without one grants the whole view, as it does now, so no rule written now changes meaning then.

## 4. Matching

A rule matches an operation when all three hold:

1. The operation's action is the rule's `action`.
2. The operation's resource is in the rule's `resource` Scope, for an action that takes one (Section 2.3).
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
- Reaching an entry point needs authentication. Mission Control's MCP server also needs `mcp:use` (Section 2.3), and each MCP tool then checks the resources it touches, e.g. `mcp:run` and the run's own checks for a playbook, and `read` for a view. _Why `mcp:use`:_ it lets an admin allow someone to use Mission Control directly but not through an AI agent, whatever else they're granted.

A check carries only the resources its action's contract can match (Section 2). An action without a target carries the resource alone, even when the operation it gates has one. So each operation makes these checks:

| Operation                             | Check                                       | Resource        | Target                            |
| ------------------------------------- | ------------------------------------------- | --------------- | --------------------------------- |
| Run playbook P with no resource       | `playbook:run`                              | P               | None                              |
| Run playbook P on resource X          | `playbook:run`, then `read`                 | P, then X       | X, then none                      |
| Run playbook P through MCP            | `mcp:run`, then the checks of the run       | P               | None                              |
| Approve or cancel run R               | `playbook:approve` or `playbook:cancel`, then `read` if R had a resource | R's playbook, then R's resource | R's resource, then none |
| Invoke a plugin operation on config C | `invoke:<plugin>:<operation>`, then `read`  | C, then C       | None                              |
| Open resource X                       | `read`                                      | X               | None                              |
| Open view V, or read its rows or panels, including through MCP | `read`             | V               | None                              |
| Connect to the MCP server             | `mcp:use`                                   | None            | None                              |
| Review what a subject may do          | `access:review`                             | None            | None                              |
| Read private properties, or create, change or delete a property | `property:manage`                | None            | None                              |
| Read the event queue or recent events | `event:read`                                | None            | None                              |
| Read job history                      | `job:read`                                  | None            | None                              |
| Send a request through the Kubernetes proxy | `kubernetes-proxy:write`, else `kubernetes-proxy:read` | None | None |
| Use connection C, e.g. to send        | `connection:use`                            | C               | None                              |
| Test connection C                     | `connection:use`                            | C               | None                              |

The plugin's checks during the operation are made as the caller, except looking up a connection, which is checked as the plugin (`identities.md`, Section 3). Approving or cancelling a run checks `read` on its resource for the same reason running it does: the run, its parameters and its results are about that resource, and are only readable with it (Section 3.1).

Each check is made as the subject performing the operation: the caller, or, where there's none, the resource doing the work (`identities.md`).

## 5. Combining rules

Across all the rules that apply to a subject, from every Role they're bound to:

- An operation is allowed when any rule granted to the subject matches it, and no rule denied to them does. It's refused otherwise.
- Order doesn't matter, and it doesn't matter which Role a rule comes from.
- Granting a Role only adds access. Rules only allow (`overview.md`, "Access"): a Role is refused only by binding it with `deny: true` (`rolebindings.md`, Section 5), and a deny wins over every grant.
- Row filters follow the same rule: a subject's rows are the ones any of their granted `read` rules allows, less the ones any of their denied `read` rules matches (Section 3.1).

## 6. How changes take effect

A Role is validated against what it references: its rules, and the Scopes they name. It's never validated against the bindings that reference it. A Role change goes through even when a binding's constraint can't narrow the new rule: that rule doesn't apply through the binding, the rest of the Role does, and the binding reports it (`rolebindings.md`, Sections 3.2 and 4). The Role's status lists the bindings reporting one of its rules. That's information, not validation: it doesn't affect whether the Role is valid.

A Role that's wrong on its own is rejected (Section 3). Otherwise it's stored, and takes effect only when all its rules are valid. One invalid rule makes the whole Role invalid: it's `Ready=False` with the reason, and none of its rules apply. There is no previous version to fall back to; the Role is whatever was last written.

A Role becomes invalid when it's written with an invalid rule, when a Scope it references changes into one a rule can't accept, is deleted, or becomes invalid itself (`scopes.md`, Section 7). It becomes valid again, and its rules apply again, as soon as the cause is gone, without being re-applied.

### 6.1 Keep Roles small

Because one broken rule silences the whole Role, a rule is only as reliable as the rules beside it. Prefer several small Roles over one large one, and bind them together:

- Group rules by the Scopes they share, so a Scope change invalidates one Role, not every Role.
- After adding a rule to a Role that's bound with a constraint, check the Role's status for bindings the rule doesn't apply through (Section 6).

A subject bound to several Roles holds the union of the valid ones (Section 5), so splitting a Role changes nothing while all of them are valid.
