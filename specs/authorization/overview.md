# Authorization

Mission Control decides who may do what with three resources:

| Resource    | Says                                                                                                                      | Spec              |
| ----------- | ------------------------------------------------------------------------------------------------------------------------- | ----------------- |
| Scope       | Which resources: a named set, e.g. "the staging configs".                                                                 | `scopes.md`       |
| Role        | What may be done to them: rules like "read the staging configs" or "run the monitoring playbooks on the staging configs". | `roles.md`        |
| RoleBinding | Who gets a Role, optionally limited further, e.g. "tenant A's operators, only on tenant A's configs".                     | `rolebindings.md` |

An ExternalIdentityProvider lets an external application's users sign in with its own tokens, so RoleBindings can grant them Roles (`external-identity-providers.md`).

`identities.md` says who each check is made as: the caller, the resource doing work no one started, a plugin, an access token's owner or an agent.

`limiting-permissions.md` says how a request is limited to less than its subject may do, e.g. by an application that signs its users in through an external identity provider.

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

Scopes, Roles and RoleBindings MAY have no namespace. Through Mission Control's API and UI, an omitted or empty `metadata.namespace` MUST mean namespace-less, not `default` or the namespace Mission Control is installed in. Objects created through Kubernetes MUST retain their Kubernetes namespace; `kubectl apply` fills in the current namespace when a manifest leaves it out.

_Why:_ some access definitions belong to the whole installation, not to one team's namespace: the shipped `admin` Role, an organisation's `auditor` Role, or a Scope selecting production configs. Bindings in `payments` and `orders` should be able to use the same reviewed definition, each with its own subjects and constraints. If references can only name objects in their own namespace, operators must copy those definitions into every namespace or move the bindings and their constraint Scopes into a shared namespace. Copies can drift and require every policy change to be repeated; moving everything defeats keeping each team's bindings alongside its other configuration. Namespace-less definitions let teams share policy without either compromise.

Roles, Scopes and bindings created through the UI or API also need no Kubernetes namespace merely to exist. A binding granting the shared `auditor` Role to the security team has no natural namespace: its subjects and access aren't limited by one. Requiring a namespace adds a placement decision that changes which definitions it can reference, not what access it grants. An operator restricted to one Kubernetes namespace can reference shipped Roles from bindings in that namespace.

### Name uniqueness

Within each kind, names MUST be unique within a namespace, and separately among namespace-less objects. A namespace-less Role `auditor` and a Role `auditor` in `platform` MAY coexist. Namespace-less does not mean shipped: users MAY create their own namespace-less objects, apart from reserved shipped names ("Shipped Roles" and "Configured bindings"). The permissions required to manage these objects are unchanged ("Who may manage these resources").

_Why:_ a shared definition must not replace a team's same-named local definition, and sharing is useful for user-created objects as well as shipped ones.

### References

A RoleBinding's `role`, a Role rule's `resource.scopeRef` and `target.scopeRef`, and a binding constraint's `resource.scopeRef` and `target.scopeRef` MUST resolve as follows:

| Reference | In a namespaced object | In a namespace-less object |
| --------- | ---------------------- | -------------------------- |
| `name` | The object named `name` in the referencing object's namespace | The namespace-less object named `name` |
| `global:name` | The namespace-less object named `name` | The namespace-less object named `name` |

`global:` is a reference qualifier, not part of the object's name. It applies equally to shipped and user-created objects. References MUST contain a valid object name, optionally preceded by exactly `global:`; an empty name, an unknown prefix or a namespace-qualified path such as `platform/auditor` MUST be rejected.

_Why:_ the qualifier distinguishes two same-named objects without changing string references into structured objects. A colon cannot occur in a Kubernetes object name, so the two forms are unambiguous. Bare names retain one rule everywhere: the referencing object's own namespace, including no namespace.

There MUST NOT be fallback between namespaced and namespace-less objects. A missing reference follows the resource's existing validity rules (`roles.md`, Section 6; `rolebindings.md`, Section 4). Creating or deleting a same-named object in another namespace MUST NOT change its target. References are by namespace and name, not immutable id: recreating a deleted target with the same namespace and name restores the reference.

_Why:_ access must not silently switch between a local and shared definition when either appears or disappears. Recreating the intended target still lets related objects be applied in any order.

A rule's Scope references MUST resolve from the Role's namespace, not the binding's. Constraint Scope references MUST resolve from the binding's namespace. For example, this Kubernetes RoleBinding uses a namespace-less Role with a local constraint Scope:

```yaml
apiVersion: mission-control.flanksource.com/v1
kind: RoleBinding
metadata:
  name: platform-admins
  namespace: platform
spec:
  role: global:admin
  subjects:
    teams: [platform]
  constraint:
    resource:
      scopeRef: tenant-a
```

Here `tenant-a` means the Scope in `platform`; the shipped Role's Scopes remain namespace-less. A Role in `platform` can likewise use `scopeRef: global:all-connections` in a rule. A namespace-less binding can use `role: global:admin` or `role: admin`, with the same result.

_Why:_ a shared Role must mean the same thing wherever it is bound, while each binding can narrow it using its own Scopes.

References to a different named namespace are not supported. A namespace-less Role or RoleBinding therefore cannot reference a namespaced Role or Scope. To bind a Role kept in Kubernetes, a UI-created binding MUST use that Role's namespace. Access tokens already name Roles with explicit namespace/name pairs and can select either namespaced or namespace-less Roles (`limiting-permissions.md`, "Access tokens"). The `X-Flanksource-Scope` header still uses Scope ids.

_Why:_ `global:` enables sharing without introducing arbitrary cross-namespace references. Token references have no owning namespace and already identify their target explicitly.

An object's namespace, including its absence, MUST NOT limit the resources a Scope selects, the subjects a binding selects or the access a Role grants. Only selectors, rules and constraints do that; namespaces are not a security boundary ("Who may manage these resources").

_Why:_ `global:` selects where a definition lives, not how much access it grants. A namespace-less Scope can select one resource, and a namespaced Scope can select resources across the install.

## A note on "target"

The word appears twice, with different meanings:

- A Scope's `targets` are its selectors.
- A rule's `target` is what an action is performed against, e.g. the config a playbook runs on.

## Access

**A subject may perform an action on a resource when a RoleBinding that selects the subject gives it a Role with a rule matching the action and resource.** The few actions that take no resource, e.g. `person:manage`, need a rule with that action alone (`roles.md`, Section 2.3). Otherwise it's refused. Nothing else grants anything, apart from deprecated Permissions (`permissions.md`).

- **Every subject is checked the same way.** A person, a team member, a user of an external identity provider and a playbook acting on its own are all subjects. What kind of subject they are only decides which RoleBinding subjects can select them (`rolebindings.md`, Section 2). It never decides what they may do. Agents are the one exception: no RoleBinding selects them, so an agent may do only what its token allows (`identities.md`, Section 5).
- **Owners act on what's theirs without a grant.** A subject's own credentials, and the data it pushes, belong to its identity rather than being granted by Roles: a person manages their own access tokens (`identities.md`, Section 4), and an agent pushes and fetches its own data (`identities.md`, Section 5). Nothing else is reached this way.
- **Everything is a resource.** Every protected thing is a type a Scope selects, from configs to RoleBindings (`scopes.md`, Section 3.1). Data about a resource, such as a playbook's runs, follows it. The exceptions are named actions without a resource: accounts, the MCP server, properties, the event queue and job history (`roles.md`, Section 2.3). Properties alone are readable by every signed-in subject without a rule (`roles.md`, Section 3.1).
- **Signing in grants nothing.** A new person or an invited person can do nothing until a RoleBinding selects them, directly or through a team their login maps them into (`rolebindings.md`, Section 2.2). A new agent can only push and fetch its own data, and no binding grants it more.
- **Listings follow the same grants.** A subject lists every resource of a type their `read` rules cover whole, the resources their `read` rules select where they cover part of it, and is refused where none applies (`collection-access.md`).
- **Rules only allow.** There are no deny rules. Adding a binding never removes access, and removing one never adds any. To keep something from a subject, don't grant it: bind the narrower Role to the narrower group.
- **Admins are subjects like any other.** An admin is a subject bound to the `admin` Role. With no deny rules, nothing can take an admin's access away except removing their binding.
- **Mission Control has no identity of its own.** There's no system user or service subject that checks run as. Work no person started, such as a scheduled playbook run or a notification being sent, is checked as the playbook or notification doing it, with that resource's own grants. A plugin acts as whoever invoked it, except when looking up a connection, which is checked as the plugin (`identities.md`, Section 3).

_Why:_ every exception is something a reader of a Role or RoleBinding can't see. One rule, with no kinds of subject, no access outside the resources and no denies, means what's written is what's granted, and an access review only has to read the allow rules RoleBindings grant.

_Why no identity of its own:_ an identity that background work runs as has to hold every grant any of that work might need. Every playbook and notification running under it could then do anything, whoever wrote it. Checking each as itself makes what it may do readable from the bindings that select it.

_Why no deny rules:_ with nothing allowed by default, every restriction can be written as a narrower grant. A deny adds a second way to read every rule, a way to lock admins out, and traps such as a deny that matches nothing (Kubernetes RBAC, GitHub and Postgres are allow-only for the same reasons).

### Shipped Roles

Mission Control ships these Roles without a namespace. They're ordinary Roles, bound by ordinary RoleBindings. The only bindings Mission Control ships are `admins` and `plugin-admins`, which bind `global:admin` and `global:plugin-admin` to the `admins` team ("Configured bindings").

| Role     | Allows                                                                                       |
| -------- | -------------------------------------------------------------------------------------------- |
| `viewer` | `read` on `config`, `component`, `check`, `canary`, `playbook`, `application`, `person` and `team` |
| `admin`  | Every action on every type (`scopes.md`, Section 3.1) and every action without a resource (`roles.md`, Section 2.3), except plugin operations |
| `plugin-admin` | Every operation of the installed plugins (`invoke:<plugin>:<operation>`), on any config |

`viewer` reads the resources people operate on, and nothing that configures access, holds credentials or exposes Mission Control's internals: no connections, notifications, notification silences, scrapers, agents, Scopes, Roles or RoleBindings, and neither `event:read` nor `job:read`. _Why:_ `viewer` is the Role most often bound to everyone, so it grants only what everyone may see. Each of the excluded types takes an explicit grant, e.g. `read` on `scope`, `role` and `roleBinding` for an auditor, and so do the event queue and job history.

Each rule of a shipped Role, apart from those on actions without a resource, needs a Scope (`roles.md`, Section 1), so the Scopes they use ship with them, also without a namespace. There's one per type, named `all-<type>` in the plural, e.g. `all-configs`, `all-notification-silences`, with a single whole-type target (`scopes.md`, Section 5.2), so a rule takes one Scope for each type it covers. No action accepts views, so no shipped Scope selects them.

The shipped Scopes are part of Mission Control's interface: Roles anyone creates, with or without a namespace, MAY use them through `global:<name>`. So a shipped Scope MUST select exactly its one type, and MUST NOT be renamed, regrouped or widened. A new type MUST get a new Scope. _Why one per type:_ a Scope grouping several types would grant more to every Role using it as soon as a release added a type to it, and could make those Roles invalid, since a rule's Scope may only select types its action accepts (`roles.md`, Section 3).

A RoleBinding in any namespace, or without one, MAY bind a shipped Role using `global:<name>`. Its constraint MAY use local or namespace-less Scopes ("References"). The subjects it selects can be anyone.

Mission Control writes the shipped Roles and their Scopes when it starts, and writes `plugin-admin` again whenever a plugin is installed, upgraded or removed and its operations change. `viewer` and `admin` are exactly as shipped, and only change with Mission Control itself. `plugin-admin` exists only while some plugin has an operation, since a Role needs a rule; a binding to it has no effect in between. The shipped objects aren't Kubernetes objects and MUST NOT be changed or deleted through the API. Namespace-less Role names `admin`, `viewer` and `plugin-admin`, and namespace-less Scope names beginning with `all-`, MUST be reserved for Mission Control, even while an object isn't shipped. Same-named objects in a Kubernetes namespace remain independent and MUST NOT be overwritten by shipped objects.

_Why reserved names:_ a user-created object must not later be replaced by a shipped one and silently change what its references grant. Reserving `all-` also leaves room for new per-type Scopes. Namespace-less user objects outside the reserved names remain editable; being namespace-less alone does not make an object shipped.

_Why `plugin-admin` is kept up to date:_ it has to hold every plugin operation, including ones added after it was written, and Role rules name actions exactly (`roles.md`, Section 2). Keeping it up to date is cheaper than a pattern that would have to mean something different in it than in every other Role. _Why apart from `admin`:_ its rules come from plugins, whose operation names Mission Control doesn't control. Keeping them out of `admin` means no plugin can change what `admin` grants, or make it invalid; at worst it affects `plugin-admin`. An admin who should run plugin operations is bound to both.

### Configured bindings

Mission Control ships one team, and RoleBindings of it to `admin` and `plugin-admin`, with the shipped Roles, so an install has a first admin without built-in roles and without any email in its configuration:

```yaml
kind: Team
metadata:
  name: admins
---
kind: RoleBinding
metadata:
  name: admins
spec:
  role: global:admin
  subjects:
    teams: [admins]
---
kind: RoleBinding
metadata:
  name: plugin-admins
spec:
  role: global:plugin-admin
  subjects:
    teams: [admins]
```

These declarations are Mission Control's configuration, not manifests to apply through Kubernetes. The bindings have no namespace. They're written like the shipped Roles, when Mission Control starts and whenever a plugin's operations change ("Shipped Roles"). `plugin-admins` is shipped only while `plugin-admin` is, i.e. while some plugin has an operation. Namespace-less RoleBinding names `admins` and `plugin-admins` MUST be reserved for Mission Control, even while a binding isn't shipped, so a user binding cannot later be replaced by one with different subjects or access. They can't be changed or deleted through the API. Same-named Kubernetes RoleBindings remain independent. The Team has no namespace: a Team applied through Kubernetes under the name `admins` is overwritten. If the shipped team or bindings are missing, e.g. deleted directly in the database, they're recreated.

Only the team itself and the bindings are protected. The team's members are managed like any team's, through the UI or API, and a member removed from it stops being an admin. Managing teams and their members takes grants on `team`, which `admin` has: changing a team's members takes `update` on it.

How the team gets its first members depends on how people sign in:

| Auth mode      | Members of `admins`                                                                                                      |
| -------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Kratos, basic  | `admin@local`, the local admin account Mission Control creates. It's added on every start, as a direct member, so a team mapper never removes it (`rolebindings.md`, Section 2.2). Its password comes from Mission Control's configuration. |
| Clerk          | Organisation admins, through Clerk's default team mapper (`rolebindings.md`, Section 2.2). There's no `admin@local`.      |

Anyone else becomes an admin by joining the team, or through a RoleBinding of their own, e.g. one binding `admin` to the `oidc` subjects of an external identity provider.

_Why a team:_ the binding never changes, so it can be shipped and protected, and who's an admin is managed as team membership, where people already manage it. Configuration lists no emails, so an install is the same everywhere and nothing in it has to be kept up to date.

_Why restored at startup:_ someone has to be able to create the first RoleBinding without going through the API, and a deleted admin binding has to come back without it. In the worst case, e.g. every member removed from `admins` or the team deleted in the database, a restart puts `admin@local` back in it (Kratos, basic), and the team mapper puts organisation admins back on their next request (Clerk). It never takes more than a restart, so admins are never locked out.

The design is in `design/bootstrap.md`.

## Who may manage these resources

Scopes, Roles, RoleBindings and ExternalIdentityProviders are managed in one of two ways, and each has one rule:

- **Through Kubernetes.** Objects are trusted as written, and who may create or change them is the cluster's RBAC. Namespaces don't limit what an object can do: a Scope in any namespace can select any resource (`scopes.md`, Section 4.2), and a RoleBinding in any namespace can name any subject (`rolebindings.md`, Section 2). So write access to any of these kinds, in any namespace, is equivalent to Mission Control admin. Don't delegate it per namespace expecting isolation.
- **Through Mission Control's API.** Creating, changing or deleting one requires `create`, `update` or `delete` on its type, `scope`, `role` or `roleBinding`, which the shipped `admin` Role allows. Only Scopes, Roles and RoleBindings have an API for now. ExternalIdentityProviders are managed through Kubernetes only. There's no design reason for that: it's the scope of this first phase, and an API for them can be added later.

## Rejected or not in effect

An invalid object is either rejected or stored `Ready=False`. Which one depends on why it's invalid:

- **Rejected**: the object is wrong on its own, so no change elsewhere can make it valid. For example, a Scope target with two types or `namespace: "*"`; a Role with two rules of the same name, `action: playbook:*`, `deny: true`, or a `target` on an action that takes none; a Permission with `deny: true` (`permissions.md`); a RoleBinding with no subjects, a `people` entry that isn't an email or `*`, or an `oidc.match` that doesn't compile; an ExternalIdentityProvider with an `http` issuer or `HS256`.
- **Stored `Ready=False`**: the object is valid on its own, but not together with something it references, or with Mission Control's settings. For example, a Scope whose `agent` isn't registered; a Role whose Scope doesn't exist, is invalid, or selects types its rule can't use; a RoleBinding whose Role doesn't exist or is invalid, or whose constraint leaves none of the Role's rules applying; an ExternalIdentityProvider whose `issuer` or `name` another provider already uses.

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
- **Permissions and PermissionGroups** are deprecated, and not specified here (`permissions.md`). They have no type, so no Role can grant managing them. **TODO.**
- **Plugins. TODO.** Plugins have no type, so no Role can grant installing, changing or removing them. Since a plugin acts with its own connections for whoever may invoke it (`identities.md`, Section 3), installing one trusts its code with those connections.
- **Access reviews. TODO.** No action grants reviewing another subject's access (`/rbac/subject-access-reviews`).
- **Reading checks. TODO.** Whether a check is read through its canary, through its own membership, or both, is for flanksource/mission-control#3520. Until it's decided, `read` accepts checks (`roles.md`, Section 2).
- **The Kubernetes proxy. TODO, deliberately postponed.** The requests made through Mission Control's Kubernetes proxy have no action under these specs, so no Role or RoleBinding can grant them, and they don't work under this model. The kubeconfig download, whose token was the proxy's credential, is removed until the proxy has an action. That's a known gap, accepted for now: the feature is rarely used and isn't worth holding the rest of this design back, though some installations may rely on it. A proposed design is recorded in `design/kubernetes-proxy.md`, and is not adopted.
- **Debug endpoints. TODO, undecided.** The debug handlers Mission Control serves from duty (`AddDebugHandlers`) are checked today against `update` on the old `database.monitor` object, which no Role can grant. Whether they get an action of their own, fall under one of the actions without a resource, or are left to admins some other way, is undecided.
- **Delegated administration.** Letting a team manage the Roles and RoleBindings of its own namespace isn't supported. It needs an escalation check, so that a binding can't grant more than its author holds.
