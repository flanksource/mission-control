# Authorization

Mission Control decides who may do what with three resources:

| Resource    | Says                                                                                                                      | Spec              |
| ----------- | ------------------------------------------------------------------------------------------------------------------------- | ----------------- |
| Scope       | Which resources: a named set, e.g. "the staging configs".                                                                 | `scopes.md`       |
| Role        | What may be done to them: rules like "read the staging configs" or "run the monitoring playbooks on the staging configs". | `roles.md`        |
| RoleBinding | Who gets a Role, optionally limited further, e.g. "tenant A's operators, only on tenant A's configs".                     | `rolebindings.md` |

An ExternalIdentityProvider lets an external application's users sign in with its own tokens, so RoleBindings can grant them Roles (`external-identity-providers.md`).

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

Every Scope, Role and RoleBinding has a namespace, and it's required however the object is created. Through Mission Control's API, an object without one is rejected. Through Kubernetes, an object always has one: `kubectl apply` uses the current namespace when a manifest leaves it out. An object is identified by its namespace and name.

References between them only resolve within one namespace:

- A Role's rules reference Scopes in the Role's namespace (`roles.md`, Section 1).
- A RoleBinding's `role` names a Role in the binding's namespace, and its constraints reference Scopes in the binding's namespace (`rolebindings.md`, Sections 1 and 3).

`scopeRef` and `role` name an object, never a namespace, so they can't reach another namespace. A Role, the Scopes its rules use and the RoleBindings that grant it are always in the same namespace. To use the same Scope in two namespaces, create it in each.

The namespace only identifies the object. It doesn't limit the resources a Scope selects or the subjects a RoleBinding names ("Who may manage these resources").

### Why every object has a namespace

References only resolve within one namespace, so an object can only be used with objects in its own namespace. Where an object lives decides what it can be combined with, however it was created. A RoleBinding created in the UI for a Role managed through Kubernetes, e.g. kept in Git or shipped in a Helm chart, has to be in that Role's namespace. A Scope created in the UI can only be used by Roles in its namespace. Giving objects created through the API a namespace like any other keeps one way of naming objects, `namespace/name`, and lets objects from either source be combined.

The cost is that someone creating an object in the UI needs a namespace, which means little to them. The UI can choose it for them from what the object will be used with, e.g. the namespace of the Role being bound, instead of asking.

An object's namespace can't be changed once it exists, through Kubernetes or the API. An object created in the wrong namespace has to be created again in the right one, along with everything that references it.

Objects created through the API share namespaces with objects managed through Kubernetes, so both can claim the same namespace and name. That's accepted as a conflict to resolve, rather than avoided by keeping the two apart.

What's been considered instead:

- **An empty namespace for objects created through the API.** These kinds are namespaced in Kubernetes, so a Kubernetes object of them never has an empty namespace. Objects created through the API could then never conflict with Kubernetes ones, and the UI would never show a namespace. Rejected: an object without a namespace could only be used with other objects without one, so anything created in the UI would be cut off from policy managed through Kubernetes. A Scope created in the UI couldn't be used by a Role from Git, and a binding created in the UI couldn't grant one. People would find out after building on those objects, and the fix is to create them again in a namespace.
- **An empty namespace alongside real ones**: the UI would use the empty namespace for objects that stand alone, and a real one to combine with policy managed through Kubernetes. Rejected for now: an object created to stand alone may need combining later, so the problem above remains. What it adds, UI objects that can never conflict with Kubernetes ones, isn't worth a second kind of namespace with its own rules without a product need for it.
- **A reserved namespace for objects created through the API**, e.g. `mission-control-ui`. Rejected: it cuts UI objects off the same way an empty namespace does, and a namespace of that name can still exist in the cluster, so it doesn't rule out conflicts either.
- **Cluster-scoped Scopes, Roles and RoleBindings**, with no namespace at all. Rejected: every object would share one set of names, so conflicts remain. Kubernetes also can't switch an existing kind from namespaced to cluster-scoped without deleting it and every object of it.

### Why references stay in one namespace

A reference is only a name. Keeping it within one namespace means:

- A namespace is self-contained. Its Roles, the Scopes they use and the RoleBindings that grant them can be read and reviewed together, and what a binding grants can be worked out without looking at other namespaces.
- Editing or deleting a Scope only changes what Roles and RoleBindings in its own namespace grant.

This isn't a security boundary today: write access to these kinds in any namespace is equivalent to Mission Control admin ("Who may manage these resources"), so references across namespaces wouldn't weaken any isolation that exists now. It would become one if teams are allowed to manage the objects of their own namespace ("Not covered yet"). A reference into another namespace would then let whoever manages that namespace change what the team's bindings grant.

What's been considered instead:

- **References that can name another namespace**, e.g. a `scopeRef` with a `namespace` as well as a name. Not done: it gives up the properties above, and nothing needs it yet. It can be added later as an optional field without changing existing objects.

### When to revisit

- If objects created in the UI are meant to be independent of policy managed through Kubernetes, so that an object created in the UI can never get in the way of one applied through Kubernetes, an empty namespace for them becomes the better choice.
- If keeping a Role with its Scopes means many namespaces carry copies of the same Scope, allow references to name another namespace. If teams manage their own namespaces by then, such a reference must not let another namespace widen what a team grants.

These decisions were discussed in [#3507](https://github.com/flanksource/mission-control/pull/3507).

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
- **Stored `Ready=False`**: the object is valid on its own, but not together with something it references, or with Mission Control's settings. For example, a Scope whose `agent` isn't registered; a Role whose Scope doesn't exist, is invalid, or selects types its rule can't use, or whose rule needs row-level security while it's off; a RoleBinding whose Role doesn't exist or is invalid, or whose constraint names a rule the Role doesn't have, or doesn't fit it; an ExternalIdentityProvider whose `issuer` or `name` another provider already uses.

An object stored `Ready=False` takes effect, without being re-applied, as soon as what it references fits it. That's what lets related objects be applied in any order: a RoleBinding can be applied before its Role, and a Role before its Scopes.

How an object is rejected depends on how it's managed:

- **Through Mission Control's API**, a create or update of an object that's wrong on its own fails with `400 Bad Request`, and nothing is stored. Unknown fields are rejected too, rather than ignored. An object that's only invalid because of what it references is stored, and the response carries why it isn't in effect.
- **Through Kubernetes**, an object is rejected when it's applied if the CRD's schema can express the check, e.g. one type per Scope target. Mission Control doesn't run an admission webhook, so checks the schema can't express, e.g. compiling an `oidc.match`, are made when the object is reconciled: such an object is stored `Ready=False` with reason `Invalid`, and has no effect. It stays that way until it's fixed.

Unknown fields are an error. Through the API they're rejected with `400`. Through Kubernetes they're rejected when the client asks for strict validation, as `kubectl apply` does by default; a client that doesn't ask has them dropped by Kubernetes before Mission Control sees the object, and Mission Control acts on what was stored.

Where the specs say an object "is rejected", this is what they mean.

## Not covered yet

- **Views.** No rule can select Views, and Roles grant no rows of the tables Views generate.
- **Permissions and PermissionGroups** are the older way of granting access. They keep working as before, and can reference Scopes.
- **Delegated administration.** Letting a team manage the Roles and RoleBindings of its own namespace isn't supported. It needs an escalation check, so that a binding can't grant more than its author holds.
