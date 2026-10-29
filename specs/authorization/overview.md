# Authorization

Mission Control decides who may do what with three resources:

| Resource    | Says                                                                                                                      | Spec              |
| ----------- | ------------------------------------------------------------------------------------------------------------------------- | ----------------- |
| Scope       | Which resources: a named set, e.g. "the staging configs".                                                                 | `scopes.md`       |
| Role        | What may be done to them: rules like "read the staging configs" or "run the monitoring playbooks on the staging configs". | `roles.md`        |
| RoleBinding | Who gets a Role, or is refused it, optionally limited further, e.g. "tenant A's operators, only on tenant A's configs". | `rolebindings.md` |

An ExternalIdentityProvider lets an external application's users sign in with its own tokens, so RoleBindings can grant them Roles (`external-identity-providers.md`).

`identities.md` says who each check is made as: the caller, the resource doing work no one started, a plugin, an access token's owner or an agent.

`access-tokens.md` says what an access token is and who may create and manage one. `token-bindings.md` says how RoleBindings that select a token limit it, and who may write them.

`limiting-permissions.md` says how a request made with an access token is limited to less than its owner may do.

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

Every Scope, Role and RoleBinding has a namespace, however it's created, and is identified by its namespace and name. The API rejects an object without one; `kubectl apply` fills in the current namespace when a manifest leaves it out. The UI picks a default namespace when the user doesn't choose one.

_Why:_ every object gets the same kind of identity, whether it comes from the UI, the API or Kubernetes. The UI default means people who don't organise objects by namespace never have to think about it.

### Name uniqueness

Names are unique per kind within a namespace, so Roles `payments/auditor` and `platform/auditor` can both exist. Users can create objects in any namespace except `mission-control-builtin` and `mission-control-access-tokens`, which belong to Mission Control ("Reserved namespace").

### API addresses

For `scopes`, `roles` and `role-bindings`, GET, PUT and DELETE address one object at `/rbac/<kind>/<namespace>/<name>`. A POST to `/rbac/<kind>` MUST set `metadata.namespace` and `metadata.name`. A PUT may leave them out of the body, and the URL supplies them; if it sets them, they MUST match the URL. A GET on `/rbac/<kind>` lists every namespace, or one with `?namespace=<namespace>`. Responses always include the object's namespace and name.

_Why:_ every object has a full address, and a namespace filter selects the same objects however they were created.

### References

A RoleBinding's `role`, and the `resource.scopeRef` and `target.scopeRef` of a Role's rule or a binding's constraint, each name one object in one of two forms:

| Reference        | Names                                                  |
| ---------------- | ------------------------------------------------------ |
| `name`           | The object of that name in the referencing object's namespace |
| `namespace/name` | The object of that name in `namespace`                 |

In an object in `monitoring`, `configs` and `monitoring/configs` name the same Scope. Anything else is rejected: more than one `/`, an empty part, or a namespace or name that isn't valid.

_Why:_ `/` can't occur in a namespace or a name, so the two forms can't be confused. The bare form keeps references within one namespace short.

#### Sharing definitions across namespaces

Roles and RoleBindings can reference objects in any namespace, whether they're managed through Kubernetes or the API. Shared objects live in an ordinary namespace, e.g. a Role `shared/ops`, and can be kept in Git beside the objects that use them.

_Why:_ teams can reuse a reviewed Role or Scope instead of copying it. This is safe because namespaces don't limit who manages these objects: a grant to manage them covers the whole type, and write access to them through Kubernetes, in any namespace, already makes someone an admin ("Who may manage these resources"). So a reference across namespaces gives no one new power. Changing a shared object does change every object that uses it.

#### Missing and recreated targets

A reference never falls back to another namespace. When the object it names doesn't exist, the referencing object is stored `Ready=False`, like any object whose reference is missing (`roles.md`, Section 6; `rolebindings.md`, Section 4). Creating or deleting an object of the same name in another namespace doesn't change what a reference names. References use namespace and name, not id, so recreating a deleted object under the same namespace and name restores the reference.

_Why:_ access must not silently switch between a local and a shared object when one of them is created or deleted. Matching by name still lets related objects be applied in any order.

#### Role Scopes and binding constraints

A bare Scope in a Role's rule is in the Role's namespace, not the binding's. A bare Scope in a binding's constraint is in the binding's namespace. For example, this binding in `platform` binds the shipped `admin` Role, narrowed to a Scope of its own:

```yaml
apiVersion: mission-control.flanksource.com/v1
kind: RoleBinding
metadata:
  name: platform-admins
  namespace: platform
spec:
  role: mission-control-builtin/admin
  subjects:
    teams: [platform]
  constraint:
    resource:
      scopeRef: tenant-a
```

Here `tenant-a` is `platform/tenant-a`, while the Scopes in `admin`'s rules are in `mission-control-builtin`. A Role in `platform` can likewise use `scopeRef: monitoring/configs` or `scopeRef: mission-control-builtin/all-connections` in a rule.

_Why:_ a shared Role means the same thing wherever it's bound, and each binding can narrow it with its own Scopes.

#### Token limits

An access token is bound to each Role by namespace and name (`limiting-permissions.md`, "Access tokens"). The RoleBindings that select it are in the reserved `mission-control-access-tokens` namespace, and name the Role as `namespace/name` (`token-bindings.md`, Section 3).

_Why:_ a token has no namespace to resolve a bare name in, and the shipped namespace holds only what Mission Control ships.

### Namespaces do not limit access

A namespace doesn't limit the resources a Scope selects, the subjects a binding selects, or what a Role grants. Only selectors, rules and constraints do. Namespaces aren't a security boundary ("Who may manage these resources").

_Why:_ a namespace says where an object is kept, not how much it grants. A Scope in any namespace can select one resource or every resource in the install.

## A note on "target"

The word appears twice, with different meanings:

- A Scope's `targets` are its selectors.
- A rule's `target` is what an action is performed against, e.g. the config a playbook runs on.

## Access

**A subject may perform an action on a resource when a RoleBinding that selects the subject gives it a Role with a rule matching the action and resource, and no deny RoleBinding that selects the subject has a rule matching them** (`rolebindings.md`, Section 5). A RoleBinding that selects an access token is the one exception: it grants nothing, and only limits what the token's owner may do with it (`token-bindings.md`). The few actions that take no resource, e.g. `person:manage`, need a rule with that action alone (`roles.md`, Section 2.3). Otherwise it's refused. Nothing else grants anything; deprecated Permissions grant nothing (`permissions.md`).

- **Every subject is checked the same way.** A person, a team member, a user of an external identity provider and a playbook acting on its own are all subjects. What kind of subject they are only decides which RoleBinding subjects can select them (`rolebindings.md`, Section 2). It never decides what they may do. Agents are the one exception: no RoleBinding selects them, so an agent may do only what its token allows (`identities.md`, Section 5). Access tokens aren't subjects: a request made with one is checked as its owner, and the RoleBindings that select the token only narrow it.
- **Owners act on what's theirs without a grant.** A subject's own credentials, and the data it pushes, belong to its identity rather than being granted by Roles: a person manages their own access tokens and the bindings that limit them (`access-tokens.md`, `token-bindings.md`), and an agent pushes and fetches its own data (`identities.md`, Section 5). Nothing else is reached this way.
- **Everything is a resource.** Every protected thing is a type a Scope selects, from configs to RoleBindings (`scopes.md`, Section 3.1). Data about a resource, such as a playbook's runs, follows it. The exceptions are named actions without a resource: accounts, the MCP server, properties, the event queue, job history and the Kubernetes proxy (`roles.md`, Section 2.3). Properties alone are readable by every signed-in subject without a rule (`roles.md`, Section 3.1).
- **Signing in grants nothing.** A new person or an invited person can do nothing until a RoleBinding selects them, directly or through a team their login maps them into (`rolebindings.md`, Section 2.2). A new agent can only push and fetch its own data, and no binding grants it more.
- **Listings follow the same grants.** A subject lists every resource of a type their `read` rules cover whole, the resources their `read` rules select where they cover part of it, less what their denied `read` rules select, and is refused where none applies (`collection-access.md`).
- **Rules only allow; bindings may deny.** A Role's rules only describe operations. A RoleBinding grants them, or with `deny: true`, refuses them, and a deny wins over every allow. Adding an allow binding never removes access, and adding a deny binding never adds any. To keep something from a subject, first don't grant it: bind the narrower Role to the narrower group. A deny binding is a guardrail on top of that, for what must stay refused whatever is granted later.
- **Admins are subjects like any other.** An admin is a subject bound to the `admin` Role. A deny binding that selects them takes away what it denies, except managing RoleBindings, which a deny never covers (`rolebindings.md`, Section 5.4).
- **Mission Control has no identity of its own.** There's no system user or service subject that checks run as. Work no person started, such as a scheduled playbook run or a notification being sent, is checked as the playbook or notification doing it, with that resource's own grants. A plugin acts as whoever invoked it, except when looking up a connection, which is checked as the plugin (`identities.md`, Section 3).

### Explicit grants make access reviewable

_Why:_ every exception is something a reader of a Role or RoleBinding can't see. One rule, with no kinds of subject and no access outside the resources, means what's written is what's granted. An access review reads the rules RoleBindings grant, less the rules deny bindings refuse.

### Background work uses the resource's identity

_Why no identity of its own:_ an identity that background work runs as has to hold every grant any of that work might need. Every playbook and notification running under it could then do anything, whoever wrote it. Checking each as itself makes what it may do readable from the bindings that select it.

### Rules only allow access

_Why no deny rules:_ with nothing allowed by default, every restriction can be written as a narrower grant. A deny in a rule would add a second way to read every Role, so a Role would no longer mean the same thing wherever it's bound.

_Why deny bindings anyway:_ a narrower grant only holds until someone binds a wider Role to the same people. A deny binding says "never" about subjects, once, and holds against a mistaken grant later. It's on the binding because who is refused is a decision about subjects, which only bindings make. Its costs are in `rolebindings.md`, Sections 5.5 and 5.6: a deny that's broken fails open, and a deny doesn't cover other paths to the same data. It can't lock admins out, since it never covers RoleBindings (`rolebindings.md`, Section 5.4).

### Shipped Roles

Mission Control ships these Roles in `mission-control-builtin`. They're ordinary Roles, bound by ordinary RoleBindings. The only bindings Mission Control ships are `admins` and `plugin-admins`, also in that namespace, which bind `admin` and `plugin-admin` to the `admins` team ("Configured bindings").

| Role     | Allows                                                                                       |
| -------- | -------------------------------------------------------------------------------------------- |
| `viewer` | `read` on `config`, `component`, `check`, `canary`, `playbook`, `view`, `application`, `person` and `team` |
| `admin`  | Every action on every type (`scopes.md`, Section 3.1) and every action without a resource (`roles.md`, Section 2.3), except plugin operations |
| `access-manage` | `access:review`, `read` on `scope`, `role`, `roleBinding`, `person`, `team`, `playbook` and `connection`, and `create`, `update` and `delete` on `scope`, `role` and `roleBinding` |
| `kubernetes-proxy` | `kubernetes-proxy:read` and `kubernetes-proxy:write`. It's the Role of the access token in a downloaded kubeconfig |
| `mcp-user` | `mcp:use` |
| `plugin-admin` | Every operation of the installed plugins (`invoke:<plugin>:<operation>`), on any config |

#### Viewer access

`viewer` reads the resources people operate on, and nothing that configures access, holds credentials or exposes Mission Control's internals: no connections, notifications, notification silences, scrapers, agents, Scopes, Roles or RoleBindings, and neither `event:read` nor `job:read`. _Why:_ `viewer` is the Role most often bound to everyone, so it grants only what everyone may see. It reads views, which can show data from any source, e.g. an SQL query through a connection: a view is published for its readers, and whoever writes it decides what they see (`roles.md`, Section 3.2). Each of the excluded types takes an explicit grant, e.g. `read` on `scope`, `role` and `roleBinding` for an auditor, and so do the event queue and job history.

#### Access management

`access-manage` is for subjects who review and manage access without being bound to `admin`, e.g. through the Settings → Permissions pages. It's as privileged as `admin`: it may bind anyone, itself included, to any Role. _Why ship it:_ it bundles what those pages need, so granting them doesn't take writing a Role. `admin` already allows everything in it.

#### MCP access for tokens

`mcp-user` is for access tokens made for AI agents. A token can connect to the MCP server only when both its owner and one of its Roles allow `mcp:use` (`limiting-permissions.md`, "Access tokens"), so a token for an agent is bound to `mcp-user` next to the Roles that say what it may reach, e.g. `viewer`. The UI's MCP setup selects it when it creates a token. Binding it grants nothing to a token whose owner lacks `mcp:use`. _Why ship it:_ `admin` is the only other shipped Role with `mcp:use`, and binding a token to `admin` for MCP would lift every other limit. _Why not exempt `mcp:use` from a token's limit:_ every check made with a token is limited the same way, with no exception to learn, and a token reaches MCP only when its owner chose that for it.

#### Shared per-type Scopes

Each rule of a shipped Role, apart from those on actions without a resource, needs a Scope (`roles.md`, Section 1), so the Scopes they use ship with them in `mission-control-builtin`. There's one per type, named `all-<type>` in the plural, e.g. `all-configs`, `all-views`, `all-notification-silences`, with a single whole-type target (`scopes.md`, Section 5.2), so a rule takes one Scope for each type it covers.

The shipped Scopes are part of Mission Control's interface: Roles in any namespace MAY use them, as `mission-control-builtin/<name>`. So a shipped Scope MUST select exactly its one type, and MUST NOT be renamed, regrouped or widened. A new type MUST get a new Scope. _Why one per type:_ a Scope grouping several types would grant more to every Role using it as soon as a release added a type to it, and could make those Roles invalid, since a rule's Scope may only select types its action accepts (`roles.md`, Section 3).

#### Binding shipped Roles

A RoleBinding in any other namespace can bind a shipped Role as `mission-control-builtin/<name>`, and its constraint can use Scopes from any namespace ("References"). The subjects it selects can be anyone. The shipped objects are only in Mission Control's database, so binding them needs no Kubernetes namespace `mission-control-builtin`, and no Kubernetes permissions in one.

#### Shipped object updates

Mission Control writes the shipped Roles and their Scopes when it starts, and writes `plugin-admin` again whenever a plugin is installed, upgraded or removed and its operations change. `viewer`, `admin`, `access-manage`, `kubernetes-proxy` and `mcp-user` are exactly as shipped, and only change with Mission Control itself. `plugin-admin` exists only while some plugin has an operation, since a Role needs a rule; a binding to it has no effect in between. The shipped objects aren't Kubernetes objects, and can't be changed or deleted through the API.

#### Reserved namespace

Mission Control owns every Scope, Role and RoleBinding name in `mission-control-builtin`, including names it doesn't ship yet, and in `mission-control-access-tokens`, which holds only the RoleBindings of access tokens (`token-bindings.md`). The API MUST refuse writes to either with `409 Conflict`. A Scope, Role or RoleBinding applied to `mission-control-builtin` through Kubernetes is stored `Ready=False` with reason `Shipped`, and has no effect. One applied to `mission-control-access-tokens` is marked `Ready=False` with reason `AccessToken`, and nothing of it is stored, so it can't take a token binding's name. Other Kubernetes resources in namespaces of those names aren't affected.

_Why:_ a release can ship a new Role or Scope without replacing a user's object and silently changing what it grants, and no object applied from Git can replace a token's binding. Users can use any name in their own namespaces, including `admin` and names starting with `all-`, and those objects are unrelated to the shipped ones.

#### Plugin permissions stay separate from admin

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
  namespace: mission-control-builtin
spec:
  role: admin
  subjects:
    teams: [admins]
---
kind: RoleBinding
metadata:
  name: plugin-admins
  namespace: mission-control-builtin
spec:
  role: plugin-admin
  subjects:
    teams: [admins]
```

#### Shipped binding protection

These declarations are Mission Control's configuration, not manifests to apply through Kubernetes. They're written like the shipped Roles, when Mission Control starts and whenever a plugin's operations change ("Shipped Roles"). `plugin-admins` is shipped only while `plugin-admin` is, i.e. while some plugin has an operation. The bindings are in the reserved namespace, so they can't be changed or deleted through the API ("Reserved namespace"). Bindings of the same name in other namespaces are unrelated to them. If the team or a binding is missing, e.g. deleted directly in the database, it's recreated.

Teams have no namespace. A Team named `admins` applied through Kubernetes is stored `Ready=False` with reason `Shipped`: it doesn't replace the shipped team or change its members.

_Why:_ applying a Team through Kubernetes must not change who's an admin.

#### Admin team membership

Only the team itself and the bindings are protected. The team's members are managed like any team's, through the UI or API, and a member removed from it stops being an admin. Managing teams and their members takes grants on `team`, which `admin` has: changing a team's members takes `update` on it.

How the team gets its first members depends on how people sign in:

| Auth mode      | Members of `admins`                                                                                                      |
| -------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Kratos, basic  | `admin@local`, the local admin account Mission Control creates. It's added on every start, as a direct member, so a team mapper never removes it (`rolebindings.md`, Section 2.2). Its password comes from Mission Control's configuration. |
| Clerk          | Organisation admins, through Clerk's default team mapper (`rolebindings.md`, Section 2.2). There's no `admin@local`.      |

Anyone else becomes an admin by joining the team, or through a RoleBinding of their own, e.g. one binding `admin` to the `oidc` subjects of an external identity provider.

_Why a team:_ the binding never changes, so it can be shipped and protected, and who's an admin is managed as team membership, where people already manage it. Configuration lists no emails, so an install is the same everywhere and nothing in it has to be kept up to date.

#### Restoring admin access

_Why restored at startup:_ someone has to be able to create the first RoleBinding without going through the API, and a deleted admin binding has to come back without it. In the worst case, e.g. every member removed from `admins` or the team deleted in the database, a restart puts `admin@local` back in it (Kratos, basic), and the team mapper puts organisation admins back on their next request (Clerk). A deny binding written through the API can't stop them deleting it, since a deny never covers RoleBindings. One applied through Kubernetes is removed through Kubernetes (`rolebindings.md`, Section 5.4). It never takes more than a restart, so admins are never locked out.

The design is in `design/bootstrap.md`.

## Who may manage these resources

Scopes, Roles, RoleBindings and ExternalIdentityProviders are managed in one of two ways, and each has one rule:

- **Through Kubernetes.** Objects are trusted as written, and who may create or change them is the cluster's RBAC. Namespaces don't limit what an object can do: a Scope in any namespace can select any resource (`scopes.md`, Section 4.2), and a RoleBinding in any namespace can name any subject (`rolebindings.md`, Section 2). So write access to any of these kinds, in any namespace, is equivalent to Mission Control admin. Don't delegate it per namespace expecting isolation.
- **Through Mission Control's API.** Creating, changing or deleting one requires `create`, `update` or `delete` on its type, `scope`, `role` or `roleBinding`, which the shipped `admin` Role allows. Only Scopes, Roles and RoleBindings have an API for now. ExternalIdentityProviders are managed through Kubernetes only. There's no design reason for that: it's the scope of this first phase, and an API for them can be added later.

## Rejected or not in effect

An invalid object is either rejected or stored `Ready=False`. Which one depends on why it's invalid:

- **Rejected**: the object is wrong on its own, so no change elsewhere can make it valid. For example, a Scope target with two types or `namespace: "*"`; a Role with two rules of the same name, `action: playbook:*`, `deny: true`, or a `target` on an action that takes none; a RoleBinding with no subjects, a `people` entry that isn't an email or `*`, or an `oidc.match` that doesn't compile; an ExternalIdentityProvider with an `http` issuer or `HS256`.
- **Stored `Ready=False`**: the object is valid on its own, but not together with something it references, or with Mission Control's settings. For example, a Scope whose `agent` isn't registered; a Role whose Scope doesn't exist, is invalid, or selects types its rule can't use; a RoleBinding whose Role doesn't exist or is invalid, or whose constraint leaves none of the Role's rules applying; a deny RoleBinding whose Role's rules only select RoleBindings; an ExternalIdentityProvider whose `issuer` or `name` another provider already uses.

### Recovery after dependency changes

An object stored `Ready=False` takes effect, without being re-applied, as soon as what it references fits it. That's what lets related objects be applied in any order: a RoleBinding can be applied before its Role, and a Role before its Scopes.

### Persistence failures

`Ready=False` with reason `PersistFailed` is different: the object may be valid, but saving its latest version failed, e.g. a Scope's membership couldn't be rebuilt. The previous version stays in effect, and Kubernetes retries the save until it succeeds.

### API and Kubernetes validation

How an object is rejected depends on how it's managed:

- **Through Mission Control's API**, a create or update of an object that's wrong on its own fails with `400 Bad Request`, and nothing is stored. Unknown fields are rejected too, rather than ignored. An object that's only invalid because of what it references is stored, and the response carries why it isn't in effect.
- **Through Kubernetes**, an object is rejected when it's applied if the CRD's schema can express the check, e.g. one type per Scope target. Mission Control doesn't run an admission webhook, so checks the schema can't express, e.g. compiling an `oidc.match`, are made when the object is reconciled: such an object is stored `Ready=False` with reason `Invalid`, and has no effect. It stays that way until it's fixed.

#### Unknown fields

Unknown fields are an error. Through the API they're rejected with `400`. Through Kubernetes they're rejected when the client asks for strict validation, as `kubectl apply` does by default; a client that doesn't ask has them dropped by Kubernetes before Mission Control sees the object, and Mission Control acts on what was stored.

Where the specs say an object "is rejected", this is what they mean.

## Not covered yet

- **Incidents. Deliberately skipped.** Incidents, with their hypotheses, evidence, responders and comments, have been behind a feature flag that's been off for years, and are expected to be removed. They get no type or action, so no Role can grant them.
- **Component logs. Deliberately skipped.** Fetching a component's logs through its log selectors is behind a disabled feature flag and is expected to be removed. It gets no action, so no one can fetch them.
- **Writing views. TODO.** `create`, `update` and `delete` don't accept views (`roles.md`, Section 2), so no Role grants writing them through the API. Views are written through Kubernetes.
- **Some rows or panels of a view.** `read` grants each view whole, with every row and panel (`roles.md`, Section 3.2). Which rows of a view a subject may see is for flanksource/mission-control#3539.
- **Plugins. TODO.** Plugins have no type, so no Role can grant installing, changing or removing them. Since a plugin acts with its own connections for whoever may invoke it (`identities.md`, Section 3), installing one trusts its code with those connections.
- **Reading checks. TODO.** Whether a check is read through its canary, through its own membership, or both, is for flanksource/mission-control#3520. Until it's decided, `read` accepts checks (`roles.md`, Section 2).
- **Debug endpoints. TODO, undecided.** The debug handlers Mission Control serves from duty (`AddDebugHandlers`) are checked today against `update` on the old `database.monitor` object, which no Role can grant. Whether they get an action of their own, fall under one of the actions without a resource, or are left to admins some other way, is undecided.
- **Delegated administration.** Letting a team manage the Roles and RoleBindings of its own namespace isn't supported. It needs an escalation check, so that a binding can't grant more than its author holds. Making namespaces an administration boundary would also mean revisiting references across namespaces, and who may change the objects they name.
