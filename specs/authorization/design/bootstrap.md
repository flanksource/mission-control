# Bootstrap

Implements `overview.md`, "Configured bindings".

## Background

Nothing is allowed by default, admins included (`overview.md`, "Access"), so a new install grants nothing until a RoleBinding exists. Someone has to create the first one without going through Mission Control's API, and the admin binding has to come back if it's deleted. Configured bindings do both. This design will say how they're declared.

## Open questions

**TODO**, to be decided with the implementation. It doesn't change any requirement of the specs:

- Where configured bindings are declared, e.g. Helm values or a file Mission Control reads at startup.
- What an install declares by default, e.g. the first admin.
- How a configured binding is told apart from one created through the API or Kubernetes, and whether it can be changed or deleted through them.
