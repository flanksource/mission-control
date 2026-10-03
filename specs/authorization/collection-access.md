# Collection-level Access Specification

This specifies how Mission Control answers whether a subject may read resources of a type, as opposed to one resource, and what every listing, search, page and derived credential does with that answer.

The terms **MUST**, **MUST NOT** and **MAY** describe requirements.

## 1. Two questions

Authorization answers two different questions about reading:

- **Resource-level:** may this subject read _this_ config? The resource is in hand, and the subject's `read` rules are matched against it ([roles.md, Section 4](roles.md#4-matching)).
- **Collection-level:** may this subject read configs _at all_? Nothing is in hand. Listings, searches, the pages of the UI and derived credentials all ask it before any resource is known.

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

A guest's built-in `read` doesn't count towards `all`. It lets a guest past whole-type checks only for their listings to be filtered to their grants ([roles.md, Section 7.3](roles.md#73-guests)), so for the answer it's as if they had none: a guest with no grants is `none`, and their listing is refused rather than empty. Where that differs from [roles.md, Section 7.3](roles.md#73-guests), this document applies. A person with no built-in role is treated as a viewer, as [roles.md, Section 7.4](roles.md#74-open-questions) leaves it.

Permissions ([overview.md, "Not covered yet"](overview.md#not-covered-yet)) count too. A Permission that allows `read` counts like a rule, with its selectors in place of a Scope, type by type. A `global` target counts for every type `read` accepts, and is whole-type only when it is `name: "*"`. For connections, which can't be filtered by row ([roles.md, Section 3.1](roles.md#31-the-read-action)), a Permission that isn't whole-type gives nothing.

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

## 4. One answer

Mission Control computes the answer. Clients MUST NOT derive it themselves from the subject's rules or roles.

Every authenticated subject can obtain their **access summary**, a user of an external identity provider included, for whom it's derived from the token of the request: for each resource type, the answer for `read`, and whether they may `create`, `update` and `delete` resources of the type. The UI uses the summary, and nothing else, to decide which resource pages to show, which to mark as showing some resources only, and which write controls to offer. Listings and searches apply the same derivation on the server. So, evaluated against the same grants, a page the summary shows never answers its listing with `403 Forbidden`, and a listing that succeeds is never behind a page the summary hides. Grants change between requests: a Scope or binding is edited, a Role becomes `Ready=False`, an external identity provider user's next token matches differently. So the summary is computed on every request, never kept for a session, and a client that gets `403 Forbidden` from a listing fetches the summary again rather than treating it as a fault.

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
