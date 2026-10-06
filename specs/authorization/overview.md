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

## Default access

Nothing is allowed unless a rule allows it, apart from the built-in access every Mission Control user has, and admins, who can do everything. Signing in grants nothing else.

No rule applies to an admin, deny rules included, however a RoleBinding or Permission names them: as a person, through a team or through a built-in role. A deny rule is a guardrail for everyone else. To restrict someone, don't make them an admin.

Built-in access is defined in code, not in these resources. The built-in roles are described in `roles.md`, Section 7. Today it's what the built-in `viewer` role can do: `read` on the catalog, topology, canaries, playbooks, views, people, applications and the public database tables, as whole types. Two things follow from it:

- It's granted on whole types, and its database listings aren't filtered by row for anyone but guests. That's why deny rules on `read` aren't supported yet (`roles.md`, Section 2): a deny couldn't be enforced on those listings.
- It only applies to Mission Control's own users. Users of an external identity provider have no built-in access, and list only what their `read` rules allow (`roles.md`, Section 3.1).

## Who may manage these resources

Scopes, Roles, RoleBindings and ExternalIdentityProviders are managed in one of two ways, and each has one rule:

- **Through Kubernetes.** Objects are trusted as written, and who may create or change them is the cluster's RBAC. Namespaces don't limit what an object can do: a Scope in any namespace can select any resource (`scopes.md`, Section 4.2), and a RoleBinding in any namespace can name any subject (`rolebindings.md`, Section 2). So write access to any of these kinds, in any namespace, is equivalent to Mission Control admin. Don't delegate it per namespace expecting isolation.
- **Through Mission Control's API.** Creating or changing any of them requires `update` on the `rbac` object, which by default only admins have. Only Scopes, Roles and RoleBindings have an API for now. ExternalIdentityProviders are managed through Kubernetes only. There's no design reason for that: it's the scope of this first phase, and an API for them can be added later.

## Rejected or not in effect

An invalid object is either rejected or stored `Ready=False`. Which one depends on why it's invalid:

- **Rejected**: the object is wrong on its own, so no change elsewhere can make it valid. For example, a Scope target with two types or `namespace: "*"`; a Role named `viewer`, with two rules of the same name, `action: playbook:*`, a deny on `read`, or a `target` on an action that takes none; a RoleBinding with no subjects, a `people` entry that isn't an email, `roles: [admin]`, or an `oidc.match` that doesn't compile; an ExternalIdentityProvider with an `http` issuer or `HS256`.
- **Stored `Ready=False`**: the object is valid on its own, but not together with something it references, or with Mission Control's settings. For example, a Scope whose `agent` isn't registered; a Role whose Scope doesn't exist, is invalid, or selects types its rule can't use, or whose rule needs row-level security while it's off; a RoleBinding whose Role doesn't exist or is invalid, or whose constraint leaves none of the Role's allow rules applying; an ExternalIdentityProvider whose `issuer` or `name` another provider already uses.

An object stored `Ready=False` takes effect, without being re-applied, as soon as what it references fits it. That's what lets related objects be applied in any order: a RoleBinding can be applied before its Role, and a Role before its Scopes.

`Ready=False` with reason `PersistFailed` is different: the object may be valid, but saving its latest version failed, e.g. a Scope's membership couldn't be rebuilt. The previous version stays in effect, and Kubernetes retries the save until it succeeds.

How an object is rejected depends on how it's managed:

- **Through Mission Control's API**, a create or update of an object that's wrong on its own fails with `400 Bad Request`, and nothing is stored. Unknown fields are rejected too, rather than ignored. An object that's only invalid because of what it references is stored, and the response carries why it isn't in effect.
- **Through Kubernetes**, an object is rejected when it's applied if the CRD's schema can express the check, e.g. one type per Scope target. Mission Control doesn't run an admission webhook, so checks the schema can't express, e.g. compiling an `oidc.match`, are made when the object is reconciled: such an object is stored `Ready=False` with reason `Invalid`, and has no effect. It stays that way until it's fixed.

Unknown fields are an error. Through the API they're rejected with `400`. Through Kubernetes they're rejected when the client asks for strict validation, as `kubectl apply` does by default; a client that doesn't ask has them dropped by Kubernetes before Mission Control sees the object, and Mission Control acts on what was stored.

Where the specs say an object "is rejected", this is what they mean.

## Not covered yet

- **Views.** No rule can select Views, and Roles grant no rows of the tables Views generate.
- **Permissions and PermissionGroups** are deprecated, and not specified here (`permissions.md`).
- **Delegated administration.** Letting a team manage the Roles and RoleBindings of its own namespace isn't supported. It needs an escalation check, so that a binding can't grant more than its author holds.
