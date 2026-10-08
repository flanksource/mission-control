# Collection-level Access Specification

This specifies how Mission Control answers whether a subject may act on resources of a type, as opposed to one resource, and what every operation, listing, search, page and derived credential does with that answer.

The terms **MUST**, **MUST NOT** and **MAY** describe requirements.

## 1. Two questions

Authorization answers two different questions about reading:

- **Resource-level:** may this subject read _this_ config? The resource is in hand, and the subject's `read` rules are matched against it ([roles.md, Section 4](roles.md#4-matching)).
- **Collection-level:** may this subject read configs _at all_? Nothing is in hand. Listings, searches, the pages of the UI and derived credentials all ask it before any resource is known, and every operation on the type is gated by it (Section 3.1).

A `read` rule on a Scope that selects some configs answers the first directly, and the second only by implication. This document makes the implication explicit, so that every place that asks the collection-level question gets the same answer.

## 2. All, some or none

For every subject and every resource type a `read` rule can accept ([roles.md, Section 2](roles.md#2-actions)), the answer to "may this subject read resources of this type?" is exactly one of:

| Answer | Meaning                                                          |
| ---------- | ---------------------------------------------------------------- |
| `all` | May read every resource of the type.                             |
| `some`   | May read the resources their `read` grants select, and no other. |
| `none`     | May read no resource of the type.                                |

The answer is derived from the subject's grants alone, in this order:

1. `all`, when either:
   - the subject's built-in role grants the type as a whole and doesn't filter their listings by row, which is every built-in role but `guest` ([roles.md, Section 7](roles.md#7-built-in-roles)); or
   - a `read` rule applies to the subject whose Scope has a whole-type target of the type ([scopes.md, Section 5.2](scopes.md#52-wildcards-and-patterns)), and the binding it comes through either has no `resource` constraint or constrains it with a Scope that also has a whole-type target of the type ([Section 2.4](#24-whole-type-under-a-constraint)).
2. Otherwise `some`, when a `read` rule applies to the subject and its Scope, narrowed by the binding's constraint where there is one ([rolebindings.md, Section 3.2](rolebindings.md#32-which-rules-a-constraint-narrows)), can select resources of the type: both Scopes have a target of the type.
3. Otherwise `none`.

For admins the answer is `all` on every type ([overview.md, "Default access"](overview.md#default-access)).

A guest's built-in `read` doesn't count towards `all`. It lets a guest past whole-type checks only for their listings to be filtered to their grants ([roles.md, Section 7.3](roles.md#73-guests)), so for the answer it's as if they had none: a guest with no grants is `none`, and their listing is refused rather than empty. The same holds when a guest opens one resource: it's checked against their grants only, never passed by the built-in `read`. Where that differs from [roles.md, Section 7.3](roles.md#73-guests), this document applies. A person with no built-in role is treated as a viewer, as [roles.md, Section 7.4](roles.md#74-open-questions) leaves it.

Permissions ([overview.md, "Not covered yet"](overview.md#not-covered-yet)) count too. A Permission that allows `read` counts like a rule, with its selectors in place of a Scope, type by type. For connections, which can't be filtered by row ([roles.md, Section 3.1](roles.md#31-the-read-action)), a Permission that isn't whole-type gives nothing.

### 2.1 Derived from grants, not from resources

The answer depends on what the subject was granted, never on which resources a valid Scope matches. A subject whose only `read` rule names a Scope that currently matches nothing is `some`, not `none`. A Scope's membership changes as resources come and go ([scopes.md, Section 4.1](scopes.md#41-empty-sets)); the answer MUST NOT change with it.

**Why.** A grant on a Scope that matches nothing is almost always a Scope with a typo, or a scraper that hasn't run yet. Showing an empty listing lets the subject, and whoever is helping them, see that access was granted and the resources are missing. Reporting `none` makes it look as if nothing was granted, which hides the mistake behind the same screen a subject with no grants sees.

An invalid Scope is a different case. A Scope naming an agent that isn't registered is invalid, not empty ([scopes.md, Section 4.1](scopes.md#41-empty-sets)): it selects nothing and the rules that reference it don't apply ([roles.md, Section 6](roles.md#6-how-changes-take-effect)), so the answer is `none`. That's the existing rule, not an exception to this one.

### 2.2 Several grants

A subject often has several `read` grants on a type, from different Roles and bindings. If any one of them is a whole-type grant, the answer is `all`. Otherwise, if there is any grant on the type, the answer is `some`, however many grants there are: three Scopes on three namespaces are still a subset. What the subject sees is the union of what the grants select. A rule that doesn't apply, because its Role or binding isn't in effect ([roles.md, Section 6](roles.md#6-how-changes-take-effect); [rolebindings.md, Section 4](rolebindings.md#4-how-changes-take-effect)), gives nothing.

Roles can't deny `read` yet ([roles.md, Section 2](roles.md#2-actions)), but a Permission can, and its denies are enforced on listings ([roles.md, Section 5](roles.md#5-allow-and-deny)). A deny lowers the answer: a deny that covers the type whole gives `none`; any other deny on the type caps the answer at `some`. The same rule applies to Role rules once they can deny `read`.

### 2.3 Row-level security off

While row-level security is off, a `read` rule whose Scope isn't whole-type doesn't apply ([roles.md, Section 3.1](roles.md#31-the-read-action)). Neither does a rule a constraint narrows to less than the whole type ([rolebindings.md, Section 3.2](rolebindings.md#32-which-rules-a-constraint-narrows)). So the answer is then `all` or `none`, never `some`.

### 2.4 Whole-type under a constraint

A constraint narrows a rule to the resources in both Scopes ([rolebindings.md, Section 3.2](rolebindings.md#32-which-rules-a-constraint-narrows)). That's a set of resources, not a Scope, so "whole-type" has to be decided from the two Scopes: a narrowed rule is whole-type for a type exactly when the rule's Scope and the constraint's Scope each have a whole-type target of it. A Scope with a whole-type target of a type selects the type whole whatever its other targets, since targets combine with OR ([scopes.md, Section 4](scopes.md#4-membership)).

| Rule's Scope       | Constraint's Scope             | Answer |
| ------------------ | ------------------------------ | ------ |
| All configs        | No constraint                  | `all`  |
| All configs        | Tenant A's configs             | `some` |
| Tenant A's configs | All configs                    | `some` |
| All configs        | All configs                    | `all`  |
| All configs        | `name: "*"` and a tag target   | `all`  |

This is the same test [rolebindings.md, Section 3.2](rolebindings.md#32-which-rules-a-constraint-narrows) uses to decide whether a narrowed `read` rule needs row-level security, and the two MUST agree: a narrowed rule is whole-type for the answer exactly when it doesn't need row-level security.

**Why.** The one unsafe reading of step 1 is "look at the rule's Scope alone": it would list every config to a subject whose binding limits them to tenant A. Requiring an explicit whole-type target on both sides leaves no path to `all` on which a constraint is bypassed.

## 3. What each answer allows

The answer decides every collection-level operation on the type:

| Operation                               | `all`                       | `some`                                                                                                | `none`                       |
| --------------------------------------- | -------------------------------- | ------------------------------------------------------------------------------------------------------- | ---------------------------- |
| List resources of the type              | Every resource                   | The resources the subject's `read` grants select                                                        | Refused with `403 Forbidden` |
| Search across types                     | The type is searched in full     | The type is searched; only granted resources are returned                                               | The type isn't searched      |
| Open the type's page in the UI          | Shown                            | Shown, and the page says it shows only the resources granted to the subject                             | Not shown                    |
| Open one resource by direct link        | The resource-level check decides | The resource-level check decides; a resource outside the grants is refused as one that doesn't exist is | Refused with `403 Forbidden`, whether or not the resource exists |

- A `some` listing MUST return exactly the resources the subject's `read` grants select: the same resources the resource-level check would allow one by one ([roles.md, Section 3.1](roles.md#31-the-read-action)). An empty result is a result, not an error.
- Opening one resource is always a resource-level check ([roles.md, Section 4.3](roles.md#43-what-an-operation-must-provide)). The answer never grants a resource the rules don't.
- A `none` refusal is the same for every id of the type, whether or not a resource with that id exists, so it reveals nothing about which resources exist ([Section 6](#6-what-a-subject-may-learn)). A `some` subject gets the not-found response for a resource outside their grants, the same as for an id that doesn't exist.
- A search is a search however it's filtered. A search limited to one type skips the type when the answer is `none` and returns an empty result; it never returns `403`. Only an endpoint that lists one type refuses.
- A page takes its type's answer ([Section 5](#5-pages-and-types)).

### 3.1 Gates

Every operation that reads, creates, updates or deletes resources of a type is let in or refused by the subject's answer for that type and action. No such operation is decided by a check of the type as a whole object. This covers configs, components, checks, canaries, playbooks and connections. Each has its own answer, derived as in Section 2. Data that belongs to a resource, such as a config's changes, analyses and relationships, takes its resource's type. A playbook run belongs to its playbook; the resource it ran on is checked as that resource's type wherever the run shows it.

Each operation is gated at the least answer it can serve:

| Gate   | Used by an operation that                                           | `all` | `some`                       | `none`                       |
| ------ | ------------------------------------------------------------------- | ----- | ---------------------------- | ---------------------------- |
| `some` | enforces the subject's grants on every resource it returns or acts on | In    | In                           | Refused with `403 Forbidden` |
| `all`  | doesn't                                                             | In    | Refused with `403 Forbidden` | Refused with `403 Forbidden` |

An operation enforces the subject's grants on a resource when it reads the resource through the subject's row filters ([roles.md, Section 3.1](roles.md#31-the-read-action)), or checks the subject's rules against the resource in hand ([roles.md, Section 4.3](roles.md#43-what-an-operation-must-provide)).

**Row filters follow the answer.** While row-level security is on, a subject whose `read` answer for a type is `some` MUST have every listing of that type filtered by row, whatever their built-in role. The filter admits the rows their grants select and leaves out the rows their denies select. A deny it can't express as rows, e.g. a Permission's inline selectors, leaves out every row of the type. A subject whose answer is `all` MAY be left unfiltered.

**Why.** A `some` gate is only safe where the operation filters. A member's built-in access selects the type whole, so without this a member lowered to `some` by a deny would pass the gate and list every row. Leaving out every row for a deny that can't be expressed refuses what it denied rather than allowing it (`permissions.md`, "Never wider").

- An operation gated at `some` MUST NOT return or act on a resource without enforcing the subject's grants on it. A resource read from a cache shared between subjects, or from another service that doesn't apply the subject's grants, is read without them.
- An operation that can't enforce the subject's grants, e.g. one served by another service, MUST be gated at `all`.
- A search across types isn't gated. It leaves out the types answered `none` and filters the rest (Section 3).
- `create`, `update` and `delete` are only checked on whole types ([roles.md, Section 2](roles.md#2-actions)), so every write is gated at `all`.
- Other actions, such as `playbook:run` or `invoke:<plugin>:<operation>`, are always checked against the resource in hand ([roles.md, Section 4.3](roles.md#43-what-an-operation-must-provide)), so they need no gate.
- Every way into an operation is gated the same, whether it's an HTTP route or an MCP tool.
- Built-in access is counted towards the answer (Section 2), like any grant, and never checked separately.
- What no rule can select isn't a resource type, and is checked against its object, which only built-in roles and Permissions grant. That includes RBAC objects, the Kubernetes proxy, logs, notifications, database tables of no resource type, and agent pushes, which are checked against `agent-push` whatever they write.

**Why gate by the answer.** A check of the type as a whole object can't see a grant or a deny on part of the type. A deny on production configs doesn't stop a check of "read configs", which every viewer passes, so an operation behind such a check shows the denied configs. The answer accounts for both: a partial grant or deny is `some`, and an operation that can't filter refuses it. The server and the UI then decide from the same answer (Section 4).

**Why `some` is refused at an `all` gate.** Letting the subject in would show them, or let them change, the resources their grants leave out. The operations gated at `some` still serve them. This holds for denies too: a deny on deleting production configs makes an editor's `delete` answer `some`, so they can delete no config until the deny is narrowed to whole types or removed.

How operations declare their gates is the design's business (`design/operation-gates.md`).

## 4. One answer

Mission Control computes the answer. Clients MUST NOT derive it themselves from the subject's rules or roles.

Every authenticated subject can obtain their **access summary**, a user of an external identity provider included, for whom it's derived from the token of the request: for each resource type, the answer for `read`, `create`, `update` and `delete`, each one of `all`, `some` or `none`. The UI uses the summary, and nothing else, to decide which resource pages to show, which to mark as showing some resources only, and which write controls to offer. Listings and searches apply the same derivation on the server. So, evaluated against the same grants, a page the summary shows never answers its listing with `403 Forbidden`, and a listing that succeeds is never behind a page the summary hides. Grants change between requests: a Scope or binding is edited, a Role becomes `Ready=False`, an external identity provider user's next token matches differently. So the answer MUST reflect the grants in effect when the request is made. Mission Control MAY cache it, keyed by everything it's derived from: the subject, their built-in roles, the Scopes they impersonate, and for a user of an external identity provider, the claims their bindings match. It MUST drop the cached answers whenever anything changes which grants apply to a subject: a Permission, PermissionGroup, Scope, Role or RoleBinding, a team's members, a person's built-in role, or a resource that's a subject itself. A client MUST NOT keep the summary for a session, and a client that gets `403 Forbidden` fetches the summary again rather than treating it as a fault.

**Why.** The answer gates requests (Section 3.1), so a stale answer keeps granting what was revoked: a person removed from a team would keep the team's access until the entry expired.

For `create`, `update` and `delete`, the answer is derived as in [Section 2](#2-all-some-or-none), from the subject's rules for that action instead of `read`. Unlike `read`, it isn't affected by row-level security. Writes are gated at `all` (Section 3.1), so the UI offers a write control only under `all`.

**Why the server answers.** The answer is a function of rules, bindings, constraints, built-in roles and the row-level security setting. A client that re-derives it has to re-implement all of them, and drifts with every release. The bug that motivated this specification was a client treating a `some` grant as `none`, and showing a user with a valid grant a screen saying they had no access to anything.

## 5. Pages and types

| Page        | Resource types |
| ----------- | -------------- |
| Catalog     | Config         |
| Topology    | Component      |
| Health      | Canary         |
| Playbooks   | Playbook       |
| Connections | Connection     |

Pages that show no resource type, such as settings, have no such answer. How they're gated is out of scope here.

Checks are read through their canary (flanksource/mission-control#3520), so Health shows one type.

Views and dashboards aren't covered: no rule can select Views yet ([overview.md, "Not covered yet"](overview.md#not-covered-yet)).

## 6. What a subject may learn

A subject may learn the answer for each type, and so may learn that what they see is a subset. The summary MUST NOT name the Scopes, rules or bindings the answer comes from, and MUST NOT say anything about the resources excluded: not how many, not of what kind.

**Why this is accepted.** A subject who sees twelve configs in an organisation of thousands already knows they're filtered. Saying so costs nothing further, and spares them reporting the missing resources as a fault. What matters is that the excluded resources stay invisible, and the answer says nothing about them.

## 7. Example

Uma is a guest, bound to a Role with one rule: `read` on a Scope that selects configs with the tag `namespace=media`.

| Type                                           | Answer | Because                                                                           |
| ---------------------------------------------- | -------- | --------------------------------------------------------------------------------- |
| Config                                         | `some` | The rule's Scope has a config target that isn't whole-type                        |
| Component, Canary, Playbook, Connection        | `none`   | No `read` rule applies on those types, and a guest has no built-in access to them |

Uma sees the Catalog page, marked as showing some resources only, listing only the media configs. She sees no other resource page. A search returns media configs and nothing else. Opening a media config by link works; opening any other config is refused. If the media namespace hasn't been scraped yet, she sees the Catalog page with an empty list.

## 8. Not covered

- **Access tokens.** Which resources a token may read, whether it can be narrower than its owner, and whether it follows later changes to the owner's grants are for a spec of their own. **TODO.** Until then, the only requirement this document relies on is that a token never reads more than its owner.
