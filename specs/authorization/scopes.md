# Scope Specification

**Status: Proposed design.** This specifies the `Scope` resource: what it selects, how its selectors are written and validated, and how changes to it take effect.

The terms **MUST**, **MUST NOT**, and **MAY** describe requirements.

## 1. Purpose

A Scope is a named, reusable **set of resources**. It says _which_ resources, never _what may be done_ to them. A Scope grants nothing by itself.

## 2. Where Scopes are used

Other objects reference a Scope by name to say which resources they apply to:

- **Role rules**, to select the resources an action is performed on, or with.
- **RoleBinding constraints**, to narrow the rules of a Role.
- **The `X-Flanksource-Scope` header**, to limit one request (`limiting-permissions.md`).

Each of those decides which resource types it accepts from a Scope, and is specified with the object itself (`roles.md`, `rolebindings.md`, `limiting-permissions.md`). This document only covers the Scope.

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
| `metadata.namespace` | Required                  | Namespace of the Scope object. Objects in other namespaces reference it as `namespace/name`. |
| `spec.description`   | Optional                  | Explanatory text; no effect.                                                        |
| `spec.targets`       | Required; 1 to 10 entries | The selectors. Each entry selects resources of exactly one type.                    |

### 3.1 Resource types

Each entry under `targets` MUST set exactly one of these keys. The key decides which fields the selector may use, and whether it can pick some resources of the type or only all of them:

| Key                   | Selects               | Fields                                                                      | Some or all                  |
| --------------------- | --------------------- | --------------------------------------------------------------------------- | ---------------------------- |
| `config`              | Config items          | `id`, `name`, `namespace`, `agent`, `types`, `tagSelector`, `labelSelector` | Some or all                  |
| `component`           | Topology components   | `id`, `name`, `namespace`, `agent`, `types`, `labelSelector`                | Some or all                  |
| `check`               | Health checks         | `id`, `name`, `namespace`, `agent`, `types`, `labelSelector`                | Some or all                  |
| `canary`              | Canaries              | `id`, `name`, `namespace`, `agent`, `labelSelector`                         | Some or all                  |
| `playbook`            | Playbooks             | `id`, `name`, `namespace`                                                   | Some or all                  |
| `connection`          | Connections           | `id`, `name`, `namespace`, `types`                                          | Some or all, except `read`   |
| `scraper`             | Config scrapers       | `id`, `name`, `namespace`, `agent`                                          | Some or all                  |
| `view`                | Views                 | `id`, `name`, `namespace`                                                   | All only, for `read`         |
| `application`         | Applications          | `name: "*"` only                                                            | All only                     |
| `notification`        | Notifications         | `name: "*"` only                                                            | All only                     |
| `notificationSilence` | Notification silences | `name: "*"` only                                                            | All only                     |
| `agent`               | Agents                | `name: "*"` only                                                            | All only                     |
| `person`              | People                | `name: "*"` only                                                            | All only                     |
| `team`                | Teams                 | `name: "*"` only                                                            | All only                     |
| `scope`               | Scopes                | `name: "*"` only                                                            | All only                     |
| `role`                | Roles                 | `name: "*"` only                                                            | All only                     |
| `roleBinding`         | RoleBindings          | `name: "*"` only                                                            | All only                     |

"All" is a whole-type target: `name: "*"` and nothing else (Section 5.2). Any other target picks some.

Whatever the type, three kinds of rule need all of it:

- **`create`, `update` and `delete`.** Mission Control checks them once per API route, never on one resource, so their Scope MUST pick whole types.
- **`read` on connections.** Connections aren't listed through the database API, so there are no row filters that could show only some of them.
- **`read` on views.** Granting some views, or some rows of a view, isn't specified yet (`roles.md`, Section 3.2).

These are the only places a whole type is required. Roles (`roles.md`, Sections 2, 3.1 and 3.2) and RoleBinding constraints (`rolebindings.md`, Section 3.2) refer to this table rather than restate it.

_Why some types are all-only:_ picking some resources of a type needs Scope membership and row filtering for that type, which only the types marked "Some or all" have. For the others, `name: "*"` is the only valid target, and every other value is rejected. Each type can get the fields its resources have, and picking some, when it's needed, without breaking a Scope written today.

_Why `view` keeps its fields:_ a `view` target that picks some views, e.g. `name: pods`, is a valid Scope, but no rule accepts it yet (`roles.md`, Section 3.2). It already means what it will mean once views can be granted in part, so nothing written now has to change then.

Everything Mission Control protects is one of these types, apart from what the few actions without a resource reach: accounts (`person:manage`), the MCP server (`mcp:use`), properties, the event queue and job history (`roles.md`, Section 2.3). There's no other kind of object a Role can grant.

Keys are exact. `playbooks`, `configs` or any other spelling is not a resource type. An entry with two keys, or none, is invalid.

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

A Scope is always a union of resources. It never expresses a relationship between them, and never references another Scope: two Scopes are only combined by an object that names both, a RoleBinding constraint (`rolebindings.md`, Section 3) or the `X-Flanksource-Scope` header (`limiting-permissions.md`).

### 4.1 Empty sets

A valid Scope that currently matches nothing is an empty set. It stays valid and selects nothing. It MUST NOT turn into "everything".

A Scope whose `agent` no longer exists isn't an empty set. It's invalid (Section 7).

### 4.2 Namespace

`metadata.namespace` identifies the Scope object and is required however it's created. Objects in the same namespace reference the Scope by name, and objects in other namespaces as `namespace/name` (`overview.md`, "References"). It does **not** restrict the resources the Scope selects. Only selectors do.

_Why:_ one Scope can be used from many namespaces without copies, and it selects the same resources wherever it's used.

### 4.3 Identity and ownership, not state

A Scope MUST select resources by what they are or who owns them, never by their current state.

Membership MUST be decidable from the Scope and the resource's own identity and ownership fields, and MUST NOT change unless the Scope or those fields change. A selector MUST NOT depend on another resource, e.g. a parent, a related resource or the members of another Scope, nor on anything outside the resource, e.g. the current time. The one exception is the name of the resource's agent (below).

**Why:** Health and status change without anyone deciding to change access. State-based grants make access disappear when a fix succeeds, flicker when a resource flaps, and grow during an outage. They can also change a playbook permission between run and approval, and prevent a stable access review.

State belongs in listing filters, e.g. "show me the unhealthy ones", and a playbook's own `configs` decide whether it should run on a resource. A Scope only decides whether the person may.

The name of the agent a resource belongs to counts as the resource's ownership, although it's stored on the agent (Section 5.4). Registering, renaming or deleting an agent therefore changes the membership of its resources, the same way a change to their own fields does.

**Why.** Mission Control stores which Scopes each resource belongs to, and only re-evaluates when the resource or the Scope changes (`design/materialised-membership.md`). That's only correct if nothing else can change membership. Every field in Section 5.1 meets this, and a field that wouldn't isn't added to the language.

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
| `*`           | Any name, including an empty one | `name: "*"`                                                    |
| `prefix*`     | Names that start with `prefix`   | `name: "prod-*"` matches `prod-db` and `prod-`, not `prod`     |
| anything else | Exactly that name                | `name: "prod-db"`                                              |

A pattern has exactly one `*`, at the end, and at least one character before it. Everything else is rejected:

- `*` at the start (`*-db`), in the middle (`prod-*-db`), at both ends (`*db*`), or more than once (`prod-*-*`).
- `*` in `namespace` or `id`.

Every character other than a trailing `*` is literal, including `,`, `!`, `%` and `_`, so there's no list or exclusion syntax: `name: "a,b"` selects a resource named `a,b`. To select several names, use one target per name; targets combine with OR (Section 4). Matching is case-sensitive: `prod-*` doesn't match `Prod-db`. A resource whose name contains `*` can only be selected by `id`.

`types` values are exact: `*` and `!` are rejected. Tag and label values can't contain `*`, so no wildcard is inferred there either.

A **whole-type target** is `name: "*"` and nothing else. It selects every resource of its type, not of every type, and it's the only target that counts as selecting a whole type (Section 3.1). A pattern target isn't one, however many resources it matches:

```yaml
targets:
  - config:
      name: "*"
```

#### Why these patterns and no others

A Scope is used in two different ways:

1. To decide whether one resource belongs to it, when that resource changes (Section 4.3).
2. To find every resource it selects, when the Scope changes. The selector becomes a database query.

Every form a `name` can take has to mean exactly the same set in both, or a resource's membership would depend on which of the two last evaluated it. And each form has to have one reading. Those two constraints rule the language down to what's above:

- **A prefix** is one comparison against the start of a name. It has one reading, and an index on the name can serve it.
- **A suffix** (`*-db`) is one comparison too, but only an index on the reversed name can serve it, so building a Scope would scan every name of its type. Select by a tag instead, e.g. `tagSelector: role=db`.
- **`*` in the middle or at both ends** (`*db*`) means searching inside every name, and a pattern with several wildcards has more than one reading. Neither is needed to select resources by name, which prefixes already do. A wider grammar would have to satisfy Section 4.3 and give each form one reading; it isn't ruled out.
- **No lists**: targets already combine with OR (Section 4), so one target per name says the same thing without a second syntax.
- **Tags and labels take `key=value` only.** `!=`, `notin` and `!key` are exclusions, ruled out below. `in` is a list, which one target per value already says. A bare `key` matches any value, a wildcard this language doesn't infer. What's left is an exact match, which has one reading and is one indexed lookup.
- **No exclusions**: an exclusion makes a selector match everything it doesn't name, including resources created later. A Scope grants access, so it names what's in, never what's out.
- **Case-sensitive** matching selects what was typed and nothing wider. Resource names from cloud providers can differ only by case, and a Scope must not quietly include both.
- **Literal `,` and `!`**: a target is a plain value, not a selector language, so they don't mean what they mean in other Mission Control selectors, e.g. a playbook's `configs`. A name written as a list or an exclusion matches no resource, so it grants nothing.
- **A pattern is not a whole type**, even one that happens to match every resource today, because what it matches changes as resources come and go. A whole-type grant must be decidable from the Scope alone.

### 5.3 Fields each type supports

A selector MUST only use the fields Section 3.1 lists for its type. Anything else is rejected, never ignored.

- A Config's namespace is its `namespace` tag. `namespace: staging` and `tagSelector: namespace=staging` select the same Configs.
- Only Configs have tags. Components, checks and canaries have labels. Playbooks, connections and scrapers have neither.

### 5.4 Agents

`agent` is an agent's name, never an id. It's stored as written, and resolved to the id of the agent registered under that name every time the Scope is validated (Section 7). The id MAY be cached between validations, but the name in the Scope is what's checked. An agent deleted and re-registered under the same name has a new id, and the Scope follows it without being updated.

**Why.** A Scope is often written apart from the agent it names, e.g. in a repository shared by several environments, where the same agent name has a different id in each. An id would only add a Scope that stops working once its agent is re-registered.

## 6. Validity

A Scope is valid when:

- It has at least one target, and at most ten. _Why ten:_ a Scope needs some limit on its targets, and ten was picked arbitrarily when Scopes were first written. There's no technical reason for the number.
- Every target sets exactly one resource type.
- Every selector has at least one condition.
- Every selector only uses fields its type supports (Section 3.1), with valid values (Sections 5.1, 5.2).
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
      name: "*"
    playbook:
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

A Scope that breaks any rule of Section 6 other than its `agent` resolving is rejected (`overview.md`, "Rejected or not in effect").

A Scope whose `agent` doesn't resolve, or that Kubernetes couldn't reject, is stored `Ready=False` with the reason, and selects nothing. There's no previous version to fall back to: the Scope is whatever was last written. Once its `agent` resolves, it's valid and selects again, without being re-applied, and the Roles and RoleBindings that reference it follow (`roles.md`, Section 6; `rolebindings.md`, Section 4).

A Scope is validated when it's saved, and again shortly after an agent is registered, renamed or deleted, since an agent can change without any Scope being saved.

A Scope is never validated against the Roles and RoleBindings that reference it. A change that makes it unusable to a rule or constraint goes through, and the referencing object stops granting, in whole or in part, until it's updated. Deleting a Scope has the same effect.

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

Saving a Scope therefore takes as long as finding every resource it selects: seconds for a broad Scope on a large tenant (`design/materialised-membership.md`).

An agent change MAY take effect shortly after it's saved, with the previous membership holding in full until then. _Why:_ agents rarely change after they're set up, and a new agent's resources arrive after it registers anyway.

### 7.2 Operations with several checks

An operation that makes several checks (`roles.md`, Section 4.3) MUST make all of them against the membership of one moment, for every resource the operation involves. It MUST NOT see a Scope's previous membership in one check and its new membership in another.

**Why.** Each check alone sees a real state, but two checks on either side of a change can together allow what neither state allows. E.g. a subject may run playbook P on configs tagged `env=dev`, and may read configs tagged `env=prod`. Config C's tag changes from `env=dev` to `env=prod`. Before, P may run on C but C can't be read; after, C can be read but P may not run on it. If the `playbook:run` check sees C before the change and the `read` check sees it after, both pass, and P runs on C.
