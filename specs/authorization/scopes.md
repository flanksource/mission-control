# Scope Specification

**Status: Proposed design.** This specifies the `Scope` resource: what it selects, how its selectors are written and validated, and how changes to it take effect.

The terms **MUST**, **MUST NOT**, and **MAY** describe requirements.

## 1. Purpose

A Scope is a named, reusable **set of resources**. It says _which_ resources, never _what may be done_ to them. A Scope grants nothing by itself.

## 2. Where Scopes are used

Other objects reference a Scope by name to say which resources they apply to:

- **Role rules**, to select the resources an action is performed on, or with.
- **RoleBinding constraints**, to narrow the rules of a Role.
- **Permissions**, through `object.scopes` (legacy).

Each of those decides which resource types, and which forms of target (e.g. whole-type only), it accepts from a Scope, and rejects a reference to a Scope it can't use. Those rules are specified with the objects themselves, in `roles.md` and `rolebindings.md`. This document only covers the Scope.

Because other objects grant access through it, editing a Scope changes what they grant. Scope administration is permission administration.

## 3. Structure

```yaml
apiVersion: mission-control.flanksource.com/v1
kind: Scope
metadata:
  name: staging
  namespace: default
spec:
  description: Staging resources
  targets:
    - config:
        tagSelector: namespace=staging
    - component:
        namespace: staging
    - check:
        namespace: staging
```

| Field                | Requirement               | Meaning                                                                             |
| -------------------- | ------------------------- | ----------------------------------------------------------------------------------- |
| `metadata.name`      | Required                  | Name other objects reference the Scope by.                                          |
| `metadata.namespace` | Required                  | Namespace of the Scope object. It is referenced from objects in the same namespace. |
| `spec.description`   | Optional                  | Explanatory text; no effect.                                                        |
| `spec.targets`       | Required; 1 to 10 entries | The selectors. Each entry selects resources of exactly one type.                    |

### 3.1 Resource types

Each entry under `targets` is either a **selector** or a **type target**:

- A selector sets exactly one of the keys below, with conditions on that type's fields (Section 5), e.g. `config: {tagSelector: env=prod}`.
- A type target is `type:` with one of the keys below as its value, e.g. `type: config`. It selects every resource of that type (Section 5.2).

The keys are:

| Key                   | Selects                                                  |
| --------------------- | -------------------------------------------------------- |
| `config`              | Config items                                             |
| `component`           | Topology components                                      |
| `check`               | Health checks                                            |
| `canary`              | Canaries                                                 |
| `playbook`            | Playbooks                                                |
| `view`                | Views                                                    |
| `connection`          | Connections                                              |
| `application`         | Applications                                             |
| `notification`        | Notifications                                            |
| `notificationSilence` | Notification silences                                    |
| `scraper`             | Config scrapers                                          |
| `agent`               | Agents                                                   |
| `person`              | People                                                   |
| `team`                | Teams                                                    |
| `scope`               | Scopes                                                   |
| `role`                | Roles                                                    |
| `roleBinding`         | RoleBindings                                             |
| `event`               | Events in Mission Control's event queue                  |
| `job`                 | Job history that belongs to no other resource (Section 3.2) |
| `property`            | Properties, Mission Control's settings and feature flags |

Everything Mission Control protects is one of these types. There's no other kind of object a Role can grant, and no global switch: "all of a type" is a whole-type target (Section 5.2).

Keys are exact. `playbooks`, `configs` or any other spelling is not a resource type. An entry with two keys, or none, is invalid, and so is a `type:` naming anything but one key.

Different entries MAY select different types.

#### Why one type per target

A target with several types would need a rule for combining them:

```yaml
targets:
  - config:
      tagSelector: cluster=beta
    playbook:
      name: echo
```

- **OR** (beta configs, or the `echo` playbook) says nothing two targets don't already say.
- **AND** (the `echo` playbook together with a beta config) pairs resources, and a Scope is only a set.

So it would either add nothing or stop being a set. Write one target per type instead.

### 3.2 Records

Some data belongs to a resource rather than being one: a config's changes and analysis, a playbook's runs, a notification's send history, a check's statuses, and job history about a resource. A record isn't a type. It's readable exactly when every resource it belongs to is (`roles.md`, Section 3.1):

- A playbook run belongs to its playbook, and to the config or check it ran on, if any.
- A run's steps, approvals and the data its agent reports belong to the run, so they follow the run.
- A relationship between two configs belongs to both.
- An artifact, e.g. a file a run's step or a check produced, belongs to what produced it: a run's step, a check, a config change or a scraper. So an artifact from a playbook run is visible exactly when the run is, whoever started the run.

**TODO:** runs on a component. Today they follow their playbook alone, so anyone who can read the playbook sees them.

**Why.** Who may see a run or a change is never a separate question from who may see what it's about. A run on a config is about the config as much as the playbook, so a subject who can't read the config can't read the run, its steps or their results. Job history about no resource has nothing to follow, so it's the type `job`.

## 4. Membership

A resource belongs to a Scope when **at least one target of the resource's type matches it**.

- Targets combine with **OR**.
- Conditions within one target's selector combine with **AND**.
- A target only ever matches resources of its own type.

```yaml
targets:
  - config:
      tagSelector: team=payments
      namespace: staging
  - config:
      namespace: development
```

This selects Configs in staging that belong to payments, **or** any Config in development. It does not select every staging Config.

A Scope is always a union of resources. It never expresses a relationship between them.

### 4.1 Empty sets

A valid Scope that currently matches nothing is an empty set. It stays valid and selects nothing. It MUST NOT turn into "everything".

A Scope whose `agent` no longer exists isn't an empty set. It's invalid (Section 7).

### 4.2 Namespace

`metadata.namespace` identifies the Scope object and is required however it's created. Roles and RoleBindings can only reference the Scope from the same namespace (`overview.md`, "Namespaces"). It does **not** restrict the resources the Scope selects. Only selectors do.

### 4.3 Identity and ownership, not state

A Scope MUST select resources by what they are or who owns them, never by their current state.

Membership MUST be decidable from the Scope and the resource's own identity and ownership fields, and MUST NOT change unless the Scope or those fields change. A selector MUST NOT depend on another resource, e.g. a parent, a related resource or the members of another Scope, nor on anything outside the resource, e.g. the current time. The one exception is the name of the resource's agent (below).

**Why:** Health and status change without anyone deciding to change access. State-based grants make access disappear when a fix succeeds, flicker when a resource flaps, and grow during an outage. They can also change a playbook permission between run and approval, and prevent a stable access review.

The grant defines the resources a person is responsible for. Listing filters narrow that set to, for example, unhealthy resources. A playbook's own configs filter decides whether it should run on a resource; a Scope decides whether the person may run it. An open-ended field selector would bypass this rule and silently expose new fields as the resource record grows.

The name of the agent a resource belongs to counts as the resource's ownership, although it's stored on the agent (Section 5.4). Registering, renaming or deleting an agent therefore changes the membership of its resources, the same way a change to their own fields does.

**Why.** Mission Control may evaluate a Scope once, when a resource or the Scope changes, and store which Scopes each resource belongs to, rather than evaluating it on every read (`design/materialised-membership.md`). That's only correct if nothing but a change to the resource, to its agent's name or to the Scope can change the resource's membership. Every field in Section 5.1 meets this. A selector that wouldn't is rejected when it's proposed for the language, never evaluated differently.

## 5. Selector language

Every target is a selector over the fields its resource type has. A selector MUST have at least one condition: an empty selector is rejected rather than meaning "everything".

### 5.1 Fields

| Field           | Matches                               | Values                                                                              |
| --------------- | ------------------------------------- | ----------------------------------------------------------------------------------- |
| `name`          | Resource name                         | One exact value, `*`, or a pattern (Section 5.2)                                    |
| `namespace`     | Resource namespace                    | One exact value                                                                     |
| `id`            | Resource id                           | A lowercase UUID                                                                    |
| `agent`         | Agent the resource belongs to         | An agent's name. MUST resolve to an existing agent (Section 5.4).                  |
| `types`         | Resource type, e.g. `Kubernetes::Pod` | A list of exact values; any of them matches                                         |
| `tagSelector`   | Tags                                  | `key=value` pairs (see below)                                                       |
| `labelSelector` | Labels                                | `key=value` pairs (see below)                                                       |

`tagSelector` and `labelSelector` take one or more `key=value` pairs joined by commas, all of which must match: `cluster=homelab,namespace=monitoring`. Every other operator of the Kubernetes label selector syntax (`==`, `!=`, `in`, `notin`, `key`, `!key`) is rejected.

### 5.2 Wildcards and patterns

`*` is the only wildcard, and `name` is the only field that takes it. `namespace` and `id` take exact values only; to match any namespace, omit `namespace`.

`name` is one of:

| Form          | Matches                          | Example                                                        |
| ------------- | -------------------------------- | -------------------------------------------------------------- |
| `prefix*`     | Names that start with `prefix`   | `name: "prod-*"` matches `prod-db` and `prod-`, not `prod`     |
| anything else | Exactly that name                | `name: "prod-db"`                                              |

A pattern has exactly one `*`, at the end, and at least one character before it. Everything else is rejected:

- `*` at the start (`*-db`), in the middle (`prod-*-db`), at both ends (`*db*`), or more than once (`prod-*-*`).
- `*` alone. To select every resource of a type, use a type target (below).
- Lists (`a,b`) and exclusions (`!a`). To select several names, use one target per name; targets combine with OR (Section 4).
- `*` in `namespace` or `id`.

Matching is case-sensitive: `prod-*` doesn't match `Prod-db`. Every character other than a trailing `*` is literal, including `,`, `!`, `%` and `_`. A resource whose name contains `*` can only be selected by `id`.

- `types` values are exact: `*` and `!` are rejected.
- Label selector values can't contain `*`, so no wildcard is inferred in `tagSelector` or `labelSelector`.

A **whole-type target** is a type target, e.g. `type: config`. It selects every resource of its type, now and later, and it's the only target that counts as selecting a whole type where that matters (`roles.md`, Sections 2 and 3.1). A selector isn't a whole-type target, however many resources it matches:

```yaml
targets:
  - type: config
```

#### Why these patterns and no others

A Scope is used in two different ways:

1. To decide whether one resource belongs to it, when that resource changes (Section 4.3).
2. To find every resource it selects, when the Scope changes. The selector becomes a database query.

Every form a `name` can take has to mean exactly the same set in both, or a resource's membership would depend on which of the two last evaluated it. And each form has to have one reading. Those two constraints rule the language down to what's above:

- **A prefix** is one comparison against the start of a name. It has one reading, and an index on the name can serve it.
- **A suffix** (`*-db`) is one comparison too, but only an index on the reversed name can serve it, so building a Scope would scan every name of its type. Select by a tag instead, e.g. `tagSelector: role=db`.
- **`*` in the middle or at both ends** (`*db*`) means searching inside every name, and a pattern with several wildcards has more than one reading. Neither is needed to select resources by name, which prefixes already do. A wider grammar would have to satisfy Section 4.3 and give each form one reading; it isn't ruled out.
- **Lists** add nothing: targets already combine with OR (Section 4), so one target per name says the same thing without a second syntax.
- **Tags and labels take `key=value` only.** `!=`, `notin` and `!key` are exclusions, ruled out below. `in` is a list, which one target per value already says. A bare `key` matches any value, a wildcard this language doesn't infer. What's left is an exact match, which has one reading and is one indexed lookup.
- **Exclusions** (`!a`) make a selector match everything it doesn't name, including resources created later. A Scope grants access, so it names what's in, never what's out.
- **Case-sensitive** matching selects what was typed and nothing wider. Resource names from cloud providers can differ only by case, and a Scope must not quietly include both.
- **Literal `,`, `!`, `%` and `_`** keep a name that contains them selectable exactly. Only `*` has a meaning.
- **A whole type is a target of its own**, not a value of `name`. A selector says which resources, a type target says all of them, and no one reading a Scope has to know that one name value means something different from the rest.
- **A pattern is not a whole type**, even one that happens to match every resource today, because what it matches changes as resources come and go. A whole-type grant must be decidable from the Scope alone.

### 5.3 Fields each type supports

A selector MUST only use fields its type has. Anything else is rejected, never ignored.

| Type         | Supported fields                                                               |
| ------------ | ------------------------------------------------------------------------------ |
| `config`     | `id`, `name`, `namespace`, `agent`, `types`, `tagSelector`, `labelSelector`        |
| `component`  | `id`, `name`, `namespace`, `agent`, `types`, `labelSelector`                       |
| `check`      | `id`, `name`, `namespace`, `agent`, `types`, `labelSelector`                       |
| `canary`     | `id`, `name`, `namespace`, `agent`, `labelSelector`                                |
| `playbook`   | `id`, `name`, `namespace`                                                        |
| `view`       | `id`, `name`, `namespace`                                                        |
| `connection` | `id`, `name`, `namespace`, `types`                                               |
| `scraper`    | `id`, `name`, `namespace`, `agent`                                               |
| `application`, `notification`, `notificationSilence`, `agent`, `person`, `team`, `scope`, `role`, `roleBinding`, `event`, `job`, `property` | None: type targets only, e.g. `type: person` |

Notes:

- A Config's namespace is its `namespace` tag. `namespace: staging` and `tagSelector: namespace=staging` select the same Configs.
- Only Configs have tags. Components, checks and canaries have labels.
- Playbooks have neither tags nor labels.
- A type with no supported fields can only be selected whole, by a type target. A selector of it, e.g. `person: {name: alice}`, is rejected. _Why:_ there's no technical reason. It's the simplest first step for types that are new to Scopes: a whole-type grant needs no selector fields, no Scope membership and no row filtering. Each type can get the fields its resources have, and partial grants, when they're needed, without breaking a Scope written today.
- Scrapers have the selector fields their resources have, since grants on some scrapers, e.g. those of one agent or namespace, are needed from the start. They have no tags or labels.

### 5.4 Agents

`agent` is an agent's name, never an id. It's stored as written, and resolved to the id of the agent registered under that name every time the Scope is validated (Section 7). The id MAY be cached between validations, but the name in the Scope is what's checked. An agent deleted and re-registered under the same name has a new id, and the Scope follows it without being updated.

**Why.** A Scope is often written apart from the agent it names, e.g. in a repository shared by several environments, where the same agent name has a different id in each. An id would only add a Scope that stops working once its agent is re-registered.

## 6. Validity

A Scope is valid when:

- It has at least one target, and at most ten.
- Every target sets exactly one resource type.
- Every selector has at least one condition.
- Every selector only uses fields its type supports (Section 5.3), with valid values (Sections 5.1, 5.2).
- Every `agent` resolves to an existing agent.

Examples of invalid Scopes:

```yaml
# Unknown resource type key
targets:
  - playbooks:
      name: restart-pod
---
# Two types in one target
targets:
  - config:
      name: prod-db
    playbook:
      name: echo
---
# A name wildcard instead of a type target: use `type: config`
targets:
  - config:
      name: "*"
---
# Empty selector: a config target without conditions
targets:
  - config:
      name: ""
---
# Wildcard outside name
targets:
  - config:
      namespace: "*"
---
# Wildcard at the start of a name: suffixes aren't supported
targets:
  - config:
      name: "*-db"
---
# Wildcard in the middle of a name
targets:
  - config:
      name: "prod-*-db"
---
# Operator other than key=value in a tag selector
targets:
  - config:
      tagSelector: "env!=prod"
---
# List of names: use one target per name instead
targets:
  - config:
      name: "prod-db,prod-api"
---
# Playbooks have no tags
targets:
  - playbook:
      tagSelector: purpose=remediation
---
# Components have labels, not tags
targets:
  - component:
      tagSelector: team=payments
```

## 7. How changes take effect

A Scope that's wrong on its own, i.e. any rule of Section 6 other than its `agent` resolving, is rejected (`overview.md`, "Rejected or not in effect"). A Scope whose `agent` doesn't resolve is stored `Ready=False`, since the agent may be registered later.

A Scope is validated (Section 6) when it's saved, and again shortly after an agent is registered, renamed or deleted. Validation on save alone isn't enough: an agent can be deleted or re-registered without any Scope being saved.

A stored Scope that becomes invalid, because its `agent` no longer resolves (Section 5.4), is `Ready=False` with the reason and selects nothing. It becomes valid again, and selects again, as soon as its `agent` resolves, without being re-applied. Roles and RoleBindings that reference it follow it (`roles.md`, Section 6; `rolebindings.md`, Section 4).

A Scope is never validated against the Roles and RoleBindings that reference it: a change that makes it unusable to a rule or constraint goes through, and the referencing object stops granting, in whole or in part, until it's updated (`roles.md`, Section 6; `rolebindings.md`, Section 4). Deleting a Scope has the same effect as changing it into one nothing accepts.

An invalid Scope that's stored, because its `agent` doesn't resolve or because Kubernetes couldn't reject it (`overview.md`, "Rejected or not in effect"), is `Ready=False` with the reason and selects nothing. There is no previous version to fall back to; the Scope is whatever was last written.

### 7.1 Membership changes

Membership MUST be decided when a Scope or a resource changes, never when a check is made (Section 4.3). A change takes effect when it's saved: the Scope or the resource commits together with its membership. Until then, the previous membership holds in full, and no check sees part of a change. Every check, of every action, MUST use the same membership.

| Change                                       | Takes effect                       |
| -------------------------------------------- | ---------------------------------- |
| A Scope is created or its targets change     | When it's saved                    |
| A Scope's `agent` resolves to a different id | Shortly after the agent changes    |
| A Scope becomes invalid or is deleted        | When it's saved or re-validated    |
| A resource is created, changed or deleted    | When it's saved                    |
| A Role or RoleBinding changes                | Immediately                        |

**Why.** A resource's membership depends only on the Scope and the resource's own fields (Section 4.3), so the write that changes either can decide it. A resource is never selected by a Scope that no longer matches it, and a playbook or a Scope applied and used straight away is never refused for being new.

Saving a Scope therefore takes as long as finding every resource it selects: seconds for a broad Scope on a large tenant, while resources wait to be saved (`design/materialised-membership.md`, notice at the top).

An agent change MAY take effect shortly after it's saved rather than with it. Until it does, the previous membership holds in full. _Why:_ agents are set up once and rarely registered, renamed or deleted again, and a new agent's resources arrive after it registers anyway.

### 7.2 Operations with several checks

An operation that makes several checks (`roles.md`, Section 4.3) MUST make all of them against the membership of one moment, for every resource the operation involves. It MUST NOT see a Scope's previous membership in one check and its new membership in another.

**Why.** Each check alone sees a real state, but two checks on either side of a change can together allow what neither state allows. E.g. a subject may run playbook P on configs tagged `env=dev`, and may read configs tagged `env=prod`. Config C's tag changes from `env=dev` to `env=prod`. Before, P may run on C but C can't be read; after, C can be read but P may not run on it. If the `playbook:run` check sees C before the change and the `read` check sees it after, both pass, and P runs on C.

## 8. Operations

A Scope never references another Scope, so two Scopes are only ever combined by an object that references them both.

### 8.1 Within a Scope

| Operation    | How                      | Example                                                             |
| ------------ | ------------------------ | ------------------------------------------------------------------- |
| Intersection | Conditions in one target | `tagSelector: team=payments` and `namespace: staging` in one target |
| Union        | Separate targets         | One target for `namespace: staging`, another for `development`      |

A Scope can't intersect two targets, exclude resources, or list names (Sections 5.1, 5.2). Targets of different types never interact.

### 8.2 Across Scopes

Two Scopes are combined in one place only: a RoleBinding constraint, which narrows a rule's Scope to the resources in both (`rolebindings.md`, Section 3). Combining grants is specified in `roles.md`, Section 5.
