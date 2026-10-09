# Bootstrap

Implements `overview.md`, "Configured bindings".

## Background

Nothing is allowed by default, admins included (`overview.md`, "Access"), so a new install grants nothing until a RoleBinding exists. Someone has to create the first one without going through Mission Control's API, and the admin binding has to come back if it's deleted. Configured bindings do both. This design will say how they're declared.

## Upgrading an existing install

Built-in roles are removed outright, with no automatic conversion. On upgrade:

- Every built-in role assignment stops granting, and its Casbin row is deleted. Admins included, nobody has access until a RoleBinding selects them.
- The first admin binding is applied by hand through Kubernetes, e.g. `kubectl apply` of a RoleBinding binding `admin` to the operator's email. Mission Control's API can't be used for it, since nobody holds a grant yet. Further bindings can then be made through the UI.
- Kratos team mapper scripts that return a `role` must be rewritten to return teams (`rolebindings.md`, Section 2.2). Clerk installs need a `--clerk-team-mapper` to replace the built-in mapping of Clerk organisation roles.

_Why by hand:_ few installations exist, and each is upgraded by its operator. Recreating their access by hand costs less than conversion code that would have to guess which teams and bindings each built-in role should become.

## Open questions

**TODO**, to be decided with the implementation. It doesn't change any requirement of the specs:

- Where configured bindings are declared, e.g. Helm values or a file Mission Control reads at startup.
- What an install declares by default, e.g. the first admin.
- How a configured binding is told apart from one created through the API or Kubernetes, and whether it can be changed or deleted through them.
