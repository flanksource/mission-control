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

The answer is derived from the subject's grants and denies alone, in this order:

0. `none`, when a deny of `read` applies to the subject over the whole type ([Section 2.5](#25-denies)).
1. `all`, when a `read` rule applies to the subject whose Scope has a whole-type target of the type ([scopes.md, Section 5.2](scopes.md#52-wildcards-and-patterns)), and the binding it comes through either has no `resource` constraint or constrains it with a Scope that also has a whole-type target of the type ([Section 2.4](#24-whole-type-under-a-constraint)).
   No deny of `read` may apply to the subject over part of the type either.
2. Otherwise `some`, when a `read` rule applies to the subject and its Scope, narrowed by the binding's constraint where there is one ([rolebindings.md, Section 3.2](rolebindings.md#32-which-rules-a-constraint-narrows)), can select resources of the type: both Scopes have a target of the type.
3. Otherwise `none`.

A subject bound to the shipped `admin` Role is `all` on every type, and one bound to `viewer` is `all` on the types it reads, through those Roles' rules ([overview.md, "Shipped Roles"](overview.md#shipped-roles)), unless a deny applies to them (Section 2.5). A subject with no grants is `none` on every type.

### 2.1 Derived from grants, not from resources

The answer depends on what the subject was granted, never on which resources a valid Scope matches. A subject whose only `read` rule names a Scope that currently matches nothing is `some`, not `none`. A Scope's membership changes as resources come and go ([scopes.md, Section 4.1](scopes.md#41-empty-sets)); the answer MUST NOT change with it.

**Why.** A grant on a Scope that matches nothing is almost always a Scope with a typo, or a scraper that hasn't run yet. Showing an empty listing lets the subject, and whoever is helping them, see that access was granted and the resources are missing. Reporting `none` makes it look as if nothing was granted, which hides the mistake behind the same screen a subject with no grants sees.

An invalid Scope is a different case. A Scope naming an agent that isn't registered is invalid, not empty ([scopes.md, Section 4.1](scopes.md#41-empty-sets)): it selects nothing and the rules that reference it don't apply ([roles.md, Section 6](roles.md#6-how-changes-take-effect)), so the answer is `none`. That's the existing rule, not an exception to this one.

### 2.2 Several grants

A subject often has several `read` grants on a type, from different Roles and bindings. If any one of them is a whole-type grant, and no deny applies (Section 2.5), the answer is `all`. Otherwise, if there is any grant on the type, the answer is `some`, however many grants there are: three Scopes on three namespaces are still a subset. What the subject sees is the union of what the grants select. A rule that doesn't apply, because its Role or binding isn't in effect ([roles.md, Section 6](roles.md#6-how-changes-take-effect); [rolebindings.md, Section 4](rolebindings.md#4-how-changes-take-effect)), gives nothing.

### 2.3 Row-level security

Row-level security is always on; there's no setting to turn it off ([roles.md, Section 3.1](roles.md#31-the-read-action)). So a `read` rule whose Scope isn't whole-type, or that a constraint narrows to less than the whole type ([rolebindings.md, Section 3.2](rolebindings.md#32-which-rules-a-constraint-narrows)), applies like any other, and makes the answer `some` (Section 2.2).

### 2.4 Whole-type under a constraint

A constraint narrows a rule to the resources in both Scopes ([rolebindings.md, Section 3.2](rolebindings.md#32-which-rules-a-constraint-narrows)). That's a set of resources, not a Scope, so "whole-type" has to be decided from the two Scopes: a narrowed rule is whole-type for a type exactly when the rule's Scope and the constraint's Scope each have a whole-type target of it. A Scope with a whole-type target of a type selects the type whole whatever its other targets, since targets combine with OR ([scopes.md, Section 4](scopes.md#4-membership)).

| Rule's Scope       | Constraint's Scope             | Answer |
| ------------------ | ------------------------------ | ------ |
| All configs        | No constraint                  | `all`  |
| All configs        | Tenant A's configs             | `some` |
| Tenant A's configs | All configs                    | `some` |
| All configs        | All configs                    | `all`  |
| All configs        | `name: "*"` and a tag target   | `all`  |

This is the same test [rolebindings.md, Section 3.2](rolebindings.md#32-which-rules-a-constraint-narrows) uses to decide whether a narrowed `read` rule filters listings by row, and the two MUST agree: a narrowed rule is whole-type for the answer exactly when it doesn't filter by row.

**Why.** The one unsafe reading of step 1 is "look at the rule's Scope alone": it would list every config to a subject whose binding limits them to tenant A. Requiring an explicit whole-type target on both sides leaves no path to `all` on which a constraint is bypassed.

### 2.5 Denies

A `read` rule of a deny binding ([rolebindings.md, Section 5](rolebindings.md#5-deny)) that applies to the subject is a deny of the resources its Scope, narrowed by the binding's constraint, selects. It's over the whole type exactly when it would be a whole-type grant (Section 2.4), and over part of the type otherwise.

| Grants         | Denies               | Answer | What's listed                         |
| -------------- | -------------------- | ------ | ------------------------------------- |
| Whole type     | None                 | `all`  | Every resource                        |
| Whole type     | Tenant A's configs   | `some` | Every resource not in tenant A        |
| Tenant A, B    | Tenant A's configs   | `some` | Tenant B's resources not in tenant A  |
| Any            | Whole type           | `none` | Refused                               |

- A `some` answer that comes from a deny lists exactly the resources the grants select and no deny matches, the same ones the resource-level check allows one by one.
- A deny of part of a type turns `all` into `some`, never `none`, even when the deny happens to match every resource. Like grants (Section 2.1), the answer follows what was written, not which resources the Scopes currently match.

**Why `some`, not `all`.** Everything that trusts `all` reads the whole type without filtering rows. A subject denied part of it must only ever be served through the paths that filter rows, which is what `some` means.

## 3. What each answer allows

The answer decides every collection-level operation on the type:

| Operation                               | `all`                       | `some`                                                                                                | `none`                       |
| --------------------------------------- | -------------------------------- | ------------------------------------------------------------------------------------------------------- | ---------------------------- |
| List resources of the type              | Every resource                   | The resources the subject's `read` grants select                                                        | Refused with `403 Forbidden` |
| Search across types                     | The type is searched in full     | The type is searched; only granted resources are returned                                               | The type isn't searched      |
| Open the type's page in the UI          | Shown                            | Shown, and the page says it shows only the resources granted to the subject                             | Not shown                    |
| Open one resource by direct link        | The resource-level check decides | The resource-level check decides; a resource outside the grants is refused as one that doesn't exist is | Refused with `403 Forbidden`, whether or not the resource exists |

- A `some` listing MUST return exactly the resources the subject's `read` grants select and none of their `read` denies matches: the same resources the resource-level check would allow one by one ([roles.md, Section 3.1](roles.md#31-the-read-action)). An empty result is a result, not an error.
- Opening one resource is always a resource-level check ([roles.md, Section 4.3](roles.md#43-what-an-operation-must-provide)). The answer never grants a resource the rules don't.
- A `none` refusal is the same for every id of the type, whether or not a resource with that id exists, so it reveals nothing about which resources exist ([Section 6](#6-what-a-subject-may-learn)). A `some` subject gets the not-found response for a resource outside their grants, the same as for an id that doesn't exist.
- A search is a search however it's filtered. A search limited to one type skips the type when the answer is `none` and returns an empty result; it never returns `403`. Only an endpoint that lists one type refuses.
- A page takes its type's answer ([Section 5](#5-pages-and-types)).

### 3.1 The event queue and job history

The event queue and job history aren't types: they're reached through `event:read` and `job:read`, actions without a resource ([roles.md, Section 2.3](roles.md#23-actions-without-a-resource)). The access summary reports these actions as booleans, not as type answers. Their listings are decided by the actions:

| Listing                       | Returns                                                                                                   |
| ----------------------------- | --------------------------------------------------------------------------------------------------------- |
| The event queue (`event_queue`) | Every event, to a subject with `event:read`. Refused with `403 Forbidden` without it                    |
| Job history (`job_history`)   | Every row, to a subject with `job:read`. Refused with `403 Forbidden` without it                          |

- Both are all or nothing. There's no per-row rule: a job history row about a resource is listed with `job:read` whether or not the subject may read the resource, and never without `job:read`, whatever resources the subject may read.
- A resource's latest job status shown in its own row, e.g. in `canaries_with_status`, isn't job history: it's listed with the resource ([roles.md, Section 3.1](roles.md#31-the-read-action)).

## 4. One answer

Mission Control computes the answer. Clients MUST NOT derive it themselves from the subject's rules or roles.

Every authenticated subject MUST receive their **access summary** from `GET /auth/whoami`, users of external identity providers included. It MUST report:

- Every type accepted by `read`, `create`, `update` or `delete` ([roles.md, Section 2](roles.md#2-actions)), with an answer for each of those actions: `all`, `some` or `none`. An action that doesn't accept the type is `none`.
- A boolean for each action without a resource: `person:manage`, `access:review`, `mcp:use`, `property:manage`, `event:read` and `job:read`. It is true exactly when the subject may perform that action.

A request made with an access token MUST receive the summary of what the token may do, the limit of the RoleBindings that select it included ([limiting-permissions.md, "Access tokens"](limiting-permissions.md#access-tokens)), for both type answers and action booleans. The one exception is `mcp:use`: connecting to MCP isn't limited by the token, yet its boolean is still computed with the token's limit, so it MAY be false for a token that can connect. _Why:_ the boolean only gates the UI, which signs in as the person rather than with a token, so a token's answer is never read.

The UI MUST use the summary alone for page, route and write-control gates, including Settings. Role names, team membership and legacy permissions MUST NOT be client-side gates. Listings and searches MUST use the same grants on the server: a page the summary shows MUST NOT have its listing refused for lack of its gate, and a permitted listing MUST NOT be hidden behind a refused page. The summary MUST reflect the grants of each request, not be kept for a session. A client receiving `403 Forbidden` from a listing MUST fetch the summary again.

**Why.** A page and its listing ask the same access question, whether it is about resources or an action without a resource. Role names say nothing about the rules a subject holds.

For `create`, `update` and `delete`, the answer is derived as in [Section 2](#2-all-some-or-none), from the subject's rules for that action instead of `read`. Their rules, and the constraints that narrow them, select whole types only ([roles.md, Section 2](roles.md#2-actions); [rolebindings.md, Section 3.2](rolebindings.md#32-which-rules-a-constraint-narrows)), so the answer is `all` or `none`, never `some`. Under `all`, the UI offers the write control. Under `none`, it MUST NOT.

**Why the server answers.** The answer is a function of rules, bindings and constraints. A client that re-derives it has to re-implement all of them, and drifts with every release. The bug that motivated this specification was a client treating a `some` grant as `none`, and showing a user with a valid grant a screen saying they had no access to anything.

## 5. Pages and types

Each page MUST have exactly one gate. Its sidebar entry and route MUST use the same gate. A type's `read` gate allows both `all` and `some`; an action gate requires true. A container such as Settings MUST be shown when it has a visible child, without a separate grant.

**Why.** Navigating to a page and opening its URL must give the same result. A navigation container grants no access of its own.

| Page                                      | Gate                         |
| ----------------------------------------- | ---------------------------- |
| Catalog, Changes, Insights, Config Access  | `read` on `config`           |
| Topology                                  | `read` on `component`        |
| Health, Health Check settings              | `read` on `canary`           |
| Playbooks, Playbook Runs                   | `read` on `playbook`         |
| Applications                              | `read` on `application`      |
| Views, Dashboard                          | `read` on `view`             |
| Connections                               | `read` on `connection`       |
| Notifications, Notification send history  | `read` on `notification`     |
| Notification Silences                     | `read` on `notificationSilence` |
| Config Scrapers                           | `read` on `scraper`          |
| Users                                     | `read` on `person`           |
| Teams                                     | `read` on `team`             |
| Agents                                    | `read` on `agent`            |
| Permissions (Roles)                       | `read` on `role`             |
| Role Bindings                             | `read` on `roleBinding`      |
| Scopes                                    | `read` on `scope`            |
| Subjects                                  | `access:review`              |
| Jobs History, including resource job tabs | `job:read`                   |
| Event Queue, including recent event tabs  | `event:read`                 |
| MCP                                       | `mcp:use`                    |
| Feature Flags                             | Authentication only         |
| Tokens                                    | Authentication only         |
| Artifacts                                 | Authentication only         |

The Permissions entry MUST open Roles. Role Bindings and Scopes MUST use their own gates, not also require `read` on `role`.

**Why.** Roles, bindings and Scopes are separate types. Reading one doesn't grant reading another.

Tokens and Artifacts MUST NOT require a page-level grant. Token operations MUST follow the ownership and credential restrictions in [access-tokens.md](access-tokens.md) and [token-bindings.md](token-bindings.md). Artifact listings and fetches MUST allow only artifacts whose producers the subject may read ([roles.md, Section 3.1](roles.md#31-the-read-action)).

**Why.** Own tokens need no grant, and artifacts inherit their producers' access. Opening either page grants no additional access.

Users' write controls MUST require `person:manage`. Feature Flags' write controls MUST require `property:manage`; reading properties needs no grant ([roles.md, Section 3.1](roles.md#31-the-read-action)). Other type write controls MUST use that type's `create`, `update` or `delete` answer (Section 4). Page gates MUST NOT replace checks on the resources shown or actions performed within them.

**Why.** Reading a page doesn't grant its writes or access to every resource it shows.

Checks are read through their canary (flanksource/mission-control#3520), so Health shows one type.

A view is granted whole, with every row and panel ([roles.md, Section 3.2](roles.md#32-views)), so a `some` answer for views lists some views, never part of one. A view shown inside another page, e.g. an application's view section, takes `read` on that view as well as that page's gate.

Integrations and Log Backends are deprecated and aren't covered here.

### 5.1 Open questions

These pages have no single gate in the model. Their gates need a decision; no new action is specified here.

| Page | Question |
| ---- | -------- |
| Plugins, including Catalog Plugins | What permits opening the plugin listing? Plugin invocation actions don't grant administration of plugins. |
| Logs | Should the page use `read` on `component`, or is a different gate needed for logs? There is no log type or action. |
| Incidents, Rules | What permits opening these pages? There is no incident or incident-rule type or action. |

## 6. What a subject may learn

A subject may learn the answer for each type, and so may learn that what they see is a subset. The summary MUST NOT name the Scopes, rules or bindings the answer comes from, and MUST NOT say anything about the resources excluded: not how many, not of what kind.

**Why this is accepted.** A subject who sees twelve configs in an organisation of thousands already knows they're filtered. Saying so costs nothing further, and spares them reporting the missing resources as a fault. What matters is that the excluded resources stay invisible, and the answer says nothing about them.

## 7. Example

Uma is bound to one Role, with one rule: `read` on a Scope that selects configs with the tag `namespace=media`.

| Type                                           | Answer | Because                                                                           |
| ---------------------------------------------- | -------- | --------------------------------------------------------------------------------- |
| Config                                         | `some` | The rule's Scope has a config target that isn't whole-type                        |
| Component, Canary, Playbook, Connection        | `none`   | No `read` rule applies on those types                                             |

Uma sees the Catalog page, marked as showing some resources only, listing only the media configs. She sees no other resource page. A search returns media configs and nothing else. Opening a media config by link works; opening any other config is refused. If the media namespace hasn't been scraped yet, she sees the Catalog page with an empty list.
