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

Each of those decides which resource types and selector fields it accepts from a Scope, and rejects a reference to a Scope it can't use. Those rules are specified with the objects themselves, in `roles.md` and `rolebindings.md`. This document only covers the Scope.

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

Each entry under `targets` MUST set exactly one of these keys:

| Key          | Selects                                                   |
| ------------ | --------------------------------------------------------- |
| `config`     | Config items                                              |
| `component`  | Topology components                                       |
| `check`      | Health checks                                             |
| `canary`     | Canaries                                                  |
| `playbook`   | Playbooks                                                 |
| `view`       | Views                                                     |
| `connection` | Connections                                               |

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

A Scope is always a union of resources. It never expresses a relationship between them.

### 4.1 Empty sets

A valid Scope that currently matches nothing is an empty set. It stays valid and selects nothing. It MUST NOT turn into "everything".

A Scope whose `agent` no longer exists isn't an empty set. It's invalid (Section 7).

### 4.2 Namespace

`metadata.namespace` identifies the Scope object and is required however it's created. Roles and RoleBindings can only reference the Scope from the same namespace (`overview.md`, "Namespaces"). It does **not** restrict the resources the Scope selects. Only selectors do.

### 4.3 Identity and ownership, not state

A Scope MUST select resources by what they are or who owns them, never by their current state.

Membership MUST be decidable from the Scope and the resource's own identity and ownership fields, and MUST NOT change unless the Scope or those fields change.

**Why:** Health and status change without anyone deciding to change access. State-based grants make access disappear when a fix succeeds, flicker when a resource flaps, and grow during an outage. They can also change a playbook permission between run and approval, and prevent a stable access review.

The grant defines the resources a person is responsible for. Listing filters narrow that set to, for example, unhealthy resources. A playbook's own configs filter decides whether it should run on a resource; a Scope decides whether the person may run it. An open-ended field selector would bypass this rule and silently expose new fields as the resource record grows.

## 5. Selector language

Every target is a selector over the fields its resource type has. A selector MUST have at least one condition: an empty selector is rejected rather than meaning "everything".

### 5.1 Fields

| Field           | Matches                               | Values                                                                              |
| --------------- | ------------------------------------- | ----------------------------------------------------------------------------------- |
| `name`          | Resource name                         | One exact value, `*`, or a pattern (Section 5.2)                                    |
| `namespace`     | Resource namespace                    | One exact value                                                                     |
| `id`            | Resource id                           | A lowercase UUID                                                                    |
| `agent`         | Agent the resource belongs to         | An agent's name or id. MUST resolve to an existing agent (Section 5.4).            |
| `types`         | Resource type, e.g. `Kubernetes::Pod` | A list of exact values; any of them matches                                         |
| `tagSelector`   | Tags                                  | Kubernetes label selector syntax                                                    |
| `labelSelector` | Labels                                | Kubernetes label selector syntax                                                    |

Kubernetes label selector syntax: `key=value`, `key!=value`, `key in (a,b)`, `key notin (a,b)`, `key`, `!key`, joined by commas (AND).

### 5.2 Wildcards and patterns

`*` is the only wildcard, and `name` is the only field that takes it. `namespace` and `id` take exact values only; to match any namespace, omit `namespace`.

`name` is one of:

| Form          | Matches                          | Example                                                        |
| ------------- | -------------------------------- | -------------------------------------------------------------- |
| `*`           | Any name, including an empty one | `name: "*"`                                                    |
| `prefix*`     | Names that start with `prefix`   | `name: "prod-*"` matches `prod-db` and `prod-`, not `prod`     |
| `*suffix`     | Names that end with `suffix`     | `name: "*-db"` matches `prod-db` and `-db`, not `db`           |
| anything else | Exactly that name                | `name: "prod-db"`                                              |

A pattern has exactly one `*`, at the start or at the end, and at least one other character. Everything else is rejected:

- `*` in the middle (`prod-*-db`), at both ends (`*db*`), or more than once (`prod-*-*`).
- Lists (`a,b`) and exclusions (`!a`). To select several names, use one target per name; targets combine with OR (Section 4).
- `*` in `namespace` or `id`.

Matching is case-sensitive: `prod-*` doesn't match `Prod-db`. Every character other than a leading or trailing `*` is literal, including `,`, `!`, `%` and `_`. A resource whose name contains `*` can only be selected by `id`.

- `name: "*"` selects every resource of the target's type, not of every type.
- `types` values are exact: `*` and `!` are rejected.
- Label selector values can't contain `*`, so no wildcard is inferred in `tagSelector` or `labelSelector`.

A **whole-type target** is `name: "*"` and nothing else. It selects every resource of its type, and it's the only target that counts as selecting a whole type where that matters (`roles.md`, Sections 2 and 3.1). A pattern target isn't a whole-type target, however many resources it matches:

```yaml
targets:
  - config:
      name: "*"
```

#### Why these patterns and no others

A Scope is matched in three different places, by three different matchers:

1. When one operation is checked against one resource, e.g. opening a config or running a playbook on it. The resource is in hand, and the selector is evaluated against it.
2. When Mission Control searches for the resources a Scope selects, e.g. to compute what a view or a playbook may show. The selector becomes a database query.
3. When a listing is filtered row by row (`roles.md`, Section 3.1). The selector becomes a row filter that runs against every row of the table.

Every form a `name` can take has to mean exactly the same set in all three, or opening a resource and listing it would disagree. And it has to be cheap in the third, which runs once per row. Those two constraints rule the language down to what's above:

- **A prefix or a suffix** is one comparison against the start or the end of a name. It costs the same as an exact match, and there's only one way to read it.
- **`*` in the middle or at both ends** (`*db*`) means searching inside every name, and a pattern with several wildcards has more than one reading. Neither is worth the cost for selecting resources by name, which is what prefixes and suffixes already do.
- **Lists** add nothing: targets already combine with OR (Section 4), so one target per name says the same thing without a second syntax.
- **Exclusions** (`!a`) make a selector match everything it doesn't name, including resources created later. A Scope grants access, so it names what's in, never what's out.
- **Case-sensitive** matching selects what was typed and nothing wider. Resource names from cloud providers can differ only by case, and a Scope must not quietly include both.
- **Literal `,`, `!`, `%` and `_`** keep a name that contains them selectable exactly. Only `*` has a meaning.
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

Notes:

- A Config's namespace is its `namespace` tag. `namespace: staging` and `tagSelector: namespace=staging` select the same Configs.
- Only Configs have tags. Components, checks and canaries have labels.
- Playbooks have neither tags nor labels.

### 5.4 Agents

`agent` is stored as written, and resolved to an agent's id every time the Scope is validated (Section 7). The id MAY be cached between validations, but the value in the Scope is what's checked.

- A **name** is resolved again on every validation. An agent deleted and re-registered under the same name has a new id, and the Scope follows it without being updated.
- An **id** names one registration. Once that agent is deleted, the Scope stays invalid until it's updated, even if an agent with the same name exists again.

Two Scopes, one written `agent: eu-cluster` and one `agent: a1000000-0000-0000-0000-000000000001`, select the same resources while that agent exists and differ once it's deleted and re-registered.

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
# Wildcard in the middle of a name
targets:
  - config:
      name: "prod-*-db"
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

A Scope is validated (Section 6) when it's applied, and again whenever an agent it references is deleted or registered. Re-validation MAY be delayed, e.g. run periodically, but a stored Scope MUST be re-validated; validation on apply alone isn't enough.

A stored Scope that becomes invalid, because its `agent` no longer resolves (Section 5.4), is `Ready=False` with the reason and selects nothing. It becomes valid again, and selects again, as soon as its `agent` resolves, without being re-applied. Roles and RoleBindings that reference it follow it (`roles.md`, Section 6; `rolebindings.md`, Section 4).

A Scope is never validated against the Roles and RoleBindings that reference it: a change that makes it unusable to a rule or constraint goes through, and the referencing object stops granting, in whole or in part, until it's updated (`roles.md`, Section 6; `rolebindings.md`, Section 4). Deleting a Scope has the same effect as changing it into one nothing accepts.

An invalid Scope that's stored, because its `agent` doesn't resolve or because Kubernetes couldn't reject it (`overview.md`, "Rejected or not in effect"), is `Ready=False` with the reason and selects nothing. There is no previous version to fall back to; the Scope is whatever was last written.
