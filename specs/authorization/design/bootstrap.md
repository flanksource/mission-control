# Bootstrap

Implements `overview.md`, "Configured bindings".

## Background

Nothing is allowed by default, admins included (`overview.md`, "Access"), so a new install grants nothing until a RoleBinding exists. Someone has to create the first one without going through Mission Control's API, and the admin binding has to come back if it's deleted. The shipped `admins` team and binding do both (`overview.md`, "Configured bindings").

## The `admins` team and bindings

They're declared with the shipped Roles and Scopes, in the YAML embedded in Mission Control (`rbac/shipped/shipped.yaml`): a Team `admins`, and namespace-less RoleBindings `admins` and `plugin-admins` binding `global:admin` and `global:plugin-admin` to `teams: [admins]`. Nothing about them is configurable. `plugin-admins` is only written while `plugin-admin` exists.

- **Told apart** by their source, `mission-control`, which the shipped Roles and Scopes have too. Their ids are derived from their kind and name, so every write updates the same rows.
- **Written** by the same sync as the shipped Roles: at startup, and whenever a plugin's operations change. The sync replaces any other team named `admins`, and recreates the shipped team and bindings if they were deleted. It never changes the team's members or same-named bindings in a Kubernetes namespace.
- **Protected.** `/rbac/role-bindings` refuses to change or delete the shipped bindings or create user bindings under their reserved namespace-less names. `/db/teams` refuses any write that could change or delete the team: writes that change or delete teams must select them by `id`, compared as UUIDs, and inserts must be JSON and are refused when they name it by id or name, e.g. an upsert. `/db/team_members` isn't affected. Through Kubernetes, a Team named `admins` is stored `Ready=False` with reason `Shipped`, and has no effect. Kubernetes RoleBindings always have a namespace and cannot collide with the shipped bindings.
- **Managed by admins.** `/db/teams` and `/db/team_members` also accept grants on `team`: `read` to read either, `create`, `update` or `delete` to write teams, and `update` to change a team's members. So someone who's an admin only through `admins` can manage its members.
- **`admin@local`.** In Kratos and basic auth, Mission Control creates `admin@local` on every start and then adds it to `admins`, with the membership's source `mission-control`. A team mapper only removes memberships it added, so it never removes this one. Its password is `ADMIN_PASSWORD`, `admin` when that isn't set: an install must set it, e.g. from a generated Kubernetes Secret.
- **Clerk.** No `admin@local`. Organisation admins join `admins` through the default Clerk team mapper (`rolebindings.md`, Section 2.2).

Until built-in roles are removed, `admin@local` also keeps its built-in admin role.

## Upgrading an existing install

Built-in roles are removed outright, with no automatic conversion. On upgrade:

- Every built-in role assignment stops granting, and its Casbin row is deleted. Admins included, nobody has access until a RoleBinding selects them.
- `admin@local` (Kratos and basic auth) and Clerk organisation admins are admins through the shipped `admins` team. Other admins sign in as `admin@local` and add themselves, or a team of theirs, to `admins`, or bind `admin` to them through the UI.
- An existing team named `admins` is replaced by the shipped one, without its members, so upgrading never makes anyone an admin who wasn't one.
- Kratos team mapper scripts that return a `role` must be rewritten to return teams (`rolebindings.md`, Section 2.2). Clerk installs get the default team mapper, which puts people in `admins` or `viewers`. `admins` is shipped and bound; create `viewers` and bind `viewer` to it to restore the old access.

_Why by hand:_ few installations exist, and each is upgraded by its operator. Recreating their access by hand costs less than conversion code that would have to guess which teams and bindings each built-in role should become.
