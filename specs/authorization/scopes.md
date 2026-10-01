# Scope Specification

**Status: Proposed design.** This specifies the `Scope` resource: what it selects, how its selectors are written and validated, and how changes to it take effect.

The terms **MUST**, **MUST NOT**, and **MAY** describe requirements.

## 1. Purpose

A Scope is a named, reusable **set of resources**. It says _which_ resources, never _what may be done_ to them. A Scope grants nothing by itself.

## 2. Where Scopes are used

Other objects reference a Scope by name to say which resources they apply to:

- **Role rules**, to select the resources an action is performed on, or with.
- **RoleBinding constraints**, to narrow a rule of a Role.
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
| `global`     | Legacy: resources of every type. Only Permissions use it. |

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

`metadata.namespace` identifies the Scope object. It does **not** restrict the resources the Scope selects. Only selectors do.

## 5. Selector language

Every target is a selector over the fields its resource type has. A selector MUST have at least one condition: an empty selector is rejected rather than meaning "everything".

### 5.1 Fields

| Field           | Matches                               | Values                                                                              |
| --------------- | ------------------------------------- | ----------------------------------------------------------------------------------- |
| `name`          | Resource name                         | One exact value, or `*`                                                             |
| `namespace`     | Resource namespace                    | One exact value                                                                     |
| `id`            | Resource id                           | A lowercase UUID                                                                    |
| `agent`         | Agent the resource belongs to         | An agent's name or id. MUST resolve to an existing agent (Section 5.5).            |
| `types`         | Resource type, e.g. `Kubernetes::Pod` | A list of exact values; any of them matches                                         |
| `statuses`      | Resource status                       | A list of exact values; any of them matches                                         |
| `health`        | Resource health                       | Comma-separated exact values; any of them matches                                   |
| `tagSelector`   | Tags                                  | Kubernetes label selector syntax                                                    |
| `labelSelector` | Labels                                | Kubernetes label selector syntax                                                    |
| `fieldSelector` | Other fields of the resource          | Kubernetes label selector syntax                                                    |

Kubernetes label selector syntax: `key=value`, `key!=value`, `key in (a,b)`, `key notin (a,b)`, `key`, `!key`, joined by commas (AND).

### 5.2 Wildcards

`*` is the only wildcard. It is only accepted as the **whole** value of `name`, and it matches any name, including an empty one. `namespace` and `id` take exact values only; to match any namespace, omit `namespace`.

- `name: "*"` selects every resource of the target's type, not of every type.
- Prefixes and suffixes (`nginx-*`), lists (`a,b`) and exclusions (`!a`) are not supported in `name`, `namespace` or `id`, and are rejected.
- `types`, `statuses` and `health` values are exact: `*` and `!` are rejected.
- Label selector values can't contain `*`, so no wildcard is inferred in `tagSelector`, `labelSelector` or `fieldSelector`.

A **whole-type target** is `name: "*"` and nothing else. It selects every resource of its type, and it's the only target that counts as selecting a whole type where that matters (`roles.md`, Sections 2 and 3.1):

```yaml
targets:
  - config:
      name: "*"
```

### 5.3 Fields each type supports

A selector MUST only use fields its type has. Anything else is rejected, never ignored.

| Type         | Supported fields                                                                                                   |
| ------------ | ------------------------------------------------------------------------------------------------------------------ |
| `config`     | `id`, `name`, `namespace`, `agent`, `types`, `statuses`, `health`, `tagSelector`, `labelSelector`, `fieldSelector` |
| `component`  | `id`, `name`, `namespace`, `agent`, `types`, `statuses`, `health`, `labelSelector`, `fieldSelector`                |
| `check`      | `id`, `name`, `namespace`, `agent`, `types`, `statuses`, `health`, `labelSelector`, `fieldSelector`                |
| `canary`     | `id`, `name`, `namespace`, `agent`, `labelSelector`                                                                |
| `playbook`   | `id`, `name`, `namespace`, `fieldSelector` on `category` only                                                      |
| `view`       | `id`, `name`, `namespace`                                                                                          |
| `connection` | `id`, `name`, `namespace`, `types`                                                                                 |
| `global`     | `id`, `name`, `namespace`, `agent`, `tagSelector`                                                                  |

Notes:

- A Config's namespace is its `namespace` tag. `namespace: staging` and `tagSelector: namespace=staging` select the same Configs.
- Only Configs have tags. Components, checks and canaries have labels.
- Playbooks have neither tags nor labels.

### 5.4 Query options

Resource selectors elsewhere in Mission Control also carry query options. They aren't conditions on a resource, so a Scope MUST reject them: `scope`, `search`, `cache`, `limit`, `includeDeleted`.

### 5.5 Agents

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
# Playbooks have no tags
targets:
  - playbook:
      tagSelector: purpose=remediation
---
# Components have labels, not tags
targets:
  - component:
      tagSelector: team=payments
---
# Query option
targets:
  - config:
      name: api
      search: type=Pod
```

## 7. Changes

A Scope that's wrong on its own, i.e. any rule of Section 6 other than its `agent` resolving, is rejected (`overview.md`, "Rejected or not in effect"). A Scope whose `agent` doesn't resolve is stored `Ready=False`, since the agent may be registered later.

A Scope is validated (Section 6) when it's applied, and again whenever an agent it references is deleted or registered. Re-validation MAY be delayed, e.g. run periodically, but a stored Scope MUST be re-validated; validation on apply alone isn't enough.

A stored Scope that becomes invalid, because its `agent` no longer resolves (Section 5.5), is `Ready=False` with the reason and selects nothing. It becomes valid again, and selects again, as soon as its `agent` resolves, without being re-applied. Roles and RoleBindings that reference it follow it (`roles.md`, Section 6; `rolebindings.md`, Section 4).

A Scope is never validated against the Roles and RoleBindings that reference it: a change that makes it unusable to a rule or constraint goes through, and the referencing object stops granting until it's updated (`roles.md`, Section 6; `rolebindings.md`, Section 4). Deleting a Scope has the same effect as changing it into one nothing accepts.

An invalid Scope that's stored, because its `agent` doesn't resolve or because Kubernetes couldn't reject it (`overview.md`, "Rejected or not in effect"), is `Ready=False` with the reason and selects nothing. There is no previous version to fall back to; the Scope is whatever was last written.
