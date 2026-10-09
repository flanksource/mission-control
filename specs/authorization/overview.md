# Authorization

Mission Control decides who may do what with three resources:

| Resource    | Says                                                                                                                      | Spec              |
| ----------- | ------------------------------------------------------------------------------------------------------------------------- | ----------------- |
| Scope       | Which resources: a named set, e.g. "the staging configs".                                                                 | `scopes.md`       |
| Role        | What may be done to them: rules like "read the staging configs" or "run the monitoring playbooks on the staging configs". | `roles.md`        |
| RoleBinding | Who gets a Role, optionally limited further, e.g. "tenant A's operators, only on tenant A's configs".                     | `rolebindings.md` |

An ExternalIdentityProvider lets an external application's users sign in with its own tokens, so RoleBindings can grant them Roles (`external-identity-providers.md`).

`collection-access.md` cuts across the three: it says how a subject's grants decide whether they may read a type at all, which is what listings, searches and the pages of the UI ask.

Each has one job. Resource selectors only live in Scopes, actions only in Roles, and subjects only in RoleBindings.

## Example

```yaml
apiVersion: mission-control.flanksource.com/v1
kind: Scope
metadata:
  name: staging-configs
  namespace: default
spec:
  targets:
    - config:
        tagSelector: env=staging
---
apiVersion: mission-control.flanksource.com/v1
kind: Scope
metadata:
  name: monitoring-playbooks
  namespace: default
spec:
  targets:
    - playbook:
        namespace: monitoring
---
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
---
apiVersion: mission-control.flanksource.com/v1
kind: RoleBinding
metadata:
  name: staging-operators
  namespace: default
spec:
  role: staging-operator
  subjects:
    teams:
      - platform
```

The `platform` team can read staging configs, and run monitoring playbooks on them.

## Namespaces

Every Scope, Role and RoleBinding has a namespace, however it's created, and is identified by its namespace and name. The API rejects an object without one; `kubectl apply` fills in the current namespace when a manifest leaves it out.

References only resolve within the referencing object's namespace: a Role's `scopeRef`s name Scopes in its namespace, and a RoleBinding's `role` and constraint `scopeRef`s name a Role and Scopes in its namespace. A reference can't name another namespace, so to use a Scope in two namespaces, create it in each. The namespace doesn't limit which resources a Scope selects or which subjects a RoleBinding names.

**Why references stay in one namespace.** This is a convention borrowed from Kubernetes RBAC, where a RoleBinding can't reference a Role in another namespace. There's no technical reason for it. It keeps a RoleBinding's Role and the Scopes they use in the binding's namespace, though the resources it grants can be in any namespace. It isn't a security boundary today, since anyone who can write these objects can grant anything ("Who may manage these resources"). So references across namespaces can be allowed later without breaking existing objects. It would only become a security boundary if teams are allowed to manage their own namespaces ("Not covered yet").

**Why objects created in the UI need a namespace too.** Since references stay in one namespace, a UI object has to be in the same namespace as the objects it's used with, e.g. a binding of a Role kept in Git. An empty namespace for UI objects would avoid name conflicts with Kubernetes objects, but would cut them off from those objects, and people would only find out when they tried to combine the two. The UI can pick the namespace instead of asking.

Full discussion: [#3507](https://github.com/flanksource/mission-control/pull/3507)

## A note on "target"

The word appears twice, with different meanings:

- A Scope's `targets` are its selectors.
- A rule's `target` is what an action is performed against, e.g. the config a playbook runs on.

## Access

**A subject may perform an action on a resource when a RoleBinding that selects the subject gives it a Role with a rule matching the action and resource.** The few actions that take no resource, e.g. `person:invite`, need a rule with that action alone (`roles.md`, Section 2.3). Otherwise it's refused. Nothing else grants anything, apart from deprecated Permissions (`permissions.md`).

- **Every subject is checked the same way.** A person, a team member, an agent, a user of an external identity provider and a playbook acting on its own are all subjects. What kind of subject they are only decides which RoleBinding subjects can select them (`rolebindings.md`, Section 2). It never decides what they may do, with one exception: an agent acts on its own data without a grant (`roles.md`, Section 4.6).
- **Everything is a resource.** Every protected thing is a type a Scope selects, from configs to RoleBindings and the event queue (`scopes.md`, Section 3.1). Data about a resource, such as a playbook's runs, follows it. Properties alone are readable by every signed-in subject without a rule (`roles.md`, Section 3.1).
- **Signing in grants nothing.** A new person or an invited person can do nothing until a RoleBinding selects them, directly or through a team their login maps them into (`rolebindings.md`, Section 2.2). A new agent can only push and fetch its own data.
- **Listings follow the same grants.** A subject lists every resource of a type their `read` rules cover whole, the resources their `read` rules select where they cover part of it, and is refused where none applies (`collection-access.md`).
- **Rules only allow.** There are no deny rules. Adding a binding never removes access, and removing one never adds any. To keep something from a subject, don't grant it: bind the narrower Role to the narrower group.
- **Admins are subjects like any other.** An admin is a subject bound to the `admin` Role. With no deny rules, nothing can take an admin's access away except removing their binding.
- **Mission Control has no identity of its own.** There's no system user or service subject that checks run as. Work no person started, such as a scheduled playbook run or a notification being sent, is checked as the playbook or notification doing it, with that resource's own grants (`roles.md`, Section 4.4).

_Why:_ every exception is something a reader of a Role or RoleBinding can't see. One rule, with no kinds of subject, no access outside the resources and no denies, means what's written is what's granted, and an access review only has to read the allow rules RoleBindings grant.

_Why no identity of its own:_ an identity that background work runs as has to hold every grant any of that work might need. Every playbook and notification running under it could then do anything, whoever wrote it. Checking each as itself makes what it may do readable from the bindings that select it.

_Why no deny rules:_ with nothing allowed by default, every restriction can be written as a narrower grant. A deny adds a second way to read every rule, a way to lock admins out, and traps such as a deny that matches nothing (Kubernetes RBAC, GitHub and Postgres are allow-only for the same reasons).

### Shipped Roles

Mission Control ships these Roles in the `mission-control` namespace. They're ordinary Roles, bound by ordinary RoleBindings; Mission Control ships no bindings to them.

| Role     | Allows                                                                                       |
| -------- | -------------------------------------------------------------------------------------------- |
| `viewer` | `read` on `config`, `component`, `check`, `canary`, `playbook`, `view`, `application`, `person` and `team` |
| `editor` | What `viewer` allows, plus `read`, `create`, `update` and `delete` on every type but `person`, `team`, `scope`, `role`, `roleBinding`, `property`, `event` and `job`, no `person:invite`, `person:manage`, `person:delete` or `mcp:use`, and `connection:use` on every connection |
| `admin`  | Every action on every type (`scopes.md`, Section 3.1), every plugin operation included       |

`viewer` reads the resources people operate on, and nothing that configures access, holds credentials or exposes Mission Control's internals: no connections, notifications, notification silences, scrapers, agents, Scopes, Roles, RoleBindings, events or job history. _Why:_ `viewer` is the Role most often bound to everyone, so it grants only what everyone may see. Each of the excluded types takes an explicit grant, e.g. `read` on `scope`, `role` and `roleBinding` for an auditor. `editor` reads every type it can change, so it can see the connections, notifications and scrapers it edits, but not the access configuration.

Each rule of a shipped Role needs a Scope (`roles.md`, Section 1), so the Scopes they use ship with them, in the same namespace: whole-type targets of every type (`scopes.md`, Section 5.2), split across as many Scopes as the ten-target limit requires.

A RoleBinding to a shipped Role MUST be created in `mission-control`, through the API or Kubernetes, since a binding's `role` only names a Role in its own namespace ("Namespaces"). The subjects it selects can be anyone.

Mission Control writes the shipped Roles and their Scopes when it starts, and again whenever the set of actions changes, e.g. when a plugin with a new operation is installed. They aren't Kubernetes objects. They can't be changed or deleted through the API, and a Role or Scope applied through Kubernetes under the same namespace and name is overwritten. _Why:_ `admin` has to hold every action, including ones added after it was written, and Role rules name actions exactly (`roles.md`, Section 2). Keeping shipped Roles up to date is cheaper than a pattern that would have to mean something different in them than in every other Role.

_Why one namespace:_ references never cross namespaces, and keeping the shipped Roles, their Scopes and the bindings to them together keeps that rule without an exception. The namespace exists on every install, with or without Kubernetes, because Mission Control writes these objects to its database itself.

### Configured bindings

Mission Control's configuration MAY declare RoleBindings, e.g. the first admins. Mission Control writes them when it starts, so they're back in place after a restart even if they were deleted.

_Why:_ someone has to be able to create the first RoleBinding, and a deleted admin binding has to come back without going through Mission Control's API. Configured bindings are RoleBindings like any other.

Where they're declared, and what an install declares by default, is left to the design (`design/bootstrap.md`).

## Who may manage these resources

Scopes, Roles, RoleBindings and ExternalIdentityProviders are managed in one of two ways, and each has one rule:

- **Through Kubernetes.** Objects are trusted as written, and who may create or change them is the cluster's RBAC. Namespaces don't limit what an object can do: a Scope in any namespace can select any resource (`scopes.md`, Section 4.2), and a RoleBinding in any namespace can name any subject (`rolebindings.md`, Section 2). So write access to any of these kinds, in any namespace, is equivalent to Mission Control admin. Don't delegate it per namespace expecting isolation.
- **Through Mission Control's API.** Creating, changing or deleting one requires `create`, `update` or `delete` on its type, `scope`, `role` or `roleBinding`, which the shipped `admin` Role allows. Only Scopes, Roles and RoleBindings have an API for now. ExternalIdentityProviders are managed through Kubernetes only. There's no design reason for that: it's the scope of this first phase, and an API for them can be added later.

## Rejected or not in effect

An invalid object is either rejected or stored `Ready=False`. Which one depends on why it's invalid:

- **Rejected**: the object is wrong on its own, so no change elsewhere can make it valid. For example, a Scope target with two types or `namespace: "*"`; a Role with two rules of the same name, `action: playbook:*`, `deny: true`, or a `target` on an action that takes none; a Permission with `deny: true` (`permissions.md`); a RoleBinding with no subjects, a `people` entry that isn't an email or `*`, or an `oidc.match` that doesn't compile; an ExternalIdentityProvider with an `http` issuer or `HS256`.
- **Stored `Ready=False`**: the object is valid on its own, but not together with something it references, or with Mission Control's settings. For example, a Scope whose `agent` isn't registered; a Role whose Scope doesn't exist, is invalid, or selects types its rule can't use, or whose rule needs row-level security while it's off; a RoleBinding whose Role doesn't exist or is invalid, or whose constraint leaves none of the Role's rules applying; an ExternalIdentityProvider whose `issuer` or `name` another provider already uses.

An object stored `Ready=False` takes effect, without being re-applied, as soon as what it references fits it. That's what lets related objects be applied in any order: a RoleBinding can be applied before its Role, and a Role before its Scopes.

`Ready=False` with reason `PersistFailed` is different: the object may be valid, but saving its latest version failed, e.g. a Scope's membership couldn't be rebuilt. The previous version stays in effect, and Kubernetes retries the save until it succeeds.

How an object is rejected depends on how it's managed:

- **Through Mission Control's API**, a create or update of an object that's wrong on its own fails with `400 Bad Request`, and nothing is stored. Unknown fields are rejected too, rather than ignored. An object that's only invalid because of what it references is stored, and the response carries why it isn't in effect.
- **Through Kubernetes**, an object is rejected when it's applied if the CRD's schema can express the check, e.g. one type per Scope target. Mission Control doesn't run an admission webhook, so checks the schema can't express, e.g. compiling an `oidc.match`, are made when the object is reconciled: such an object is stored `Ready=False` with reason `Invalid`, and has no effect. It stays that way until it's fixed.

Unknown fields are an error. Through the API they're rejected with `400`. Through Kubernetes they're rejected when the client asks for strict validation, as `kubectl apply` does by default; a client that doesn't ask has them dropped by Kubernetes before Mission Control sees the object, and Mission Control acts on what was stored.

Where the specs say an object "is rejected", this is what they mean.

## Not covered yet

- **Incidents. Deliberately skipped.** Incidents, with their hypotheses, evidence, responders and comments, have been behind a feature flag that's been off for years, and are expected to be removed. They get no type or action, so no Role can grant them.
- **Component logs. Deliberately skipped.** Fetching a component's logs through its log selectors is behind a disabled feature flag and is expected to be removed. It gets no action, so no one can fetch them.
- **Views.** No rule can select Views, and Roles grant no rows of the tables Views generate.
- **Permissions and PermissionGroups** are deprecated, and not specified here (`permissions.md`).
- **The Kubernetes proxy. TODO, deliberately postponed.** The kubeconfig download and the requests made through Mission Control's Kubernetes proxy have no action under these specs, so no Role or RoleBinding can grant them, and they don't work under this model. That's a known gap, accepted for now: the feature is rarely used and isn't worth holding the rest of this design back, though some installations may rely on it. A proposed design is recorded in `design/kubernetes-proxy.md`, and is not adopted.
- **Delegated administration.** Letting a team manage the Roles and RoleBindings of its own namespace isn't supported. It needs an escalation check, so that a binding can't grant more than its author holds.
