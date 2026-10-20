# Permissions

**Deprecated, and grant nothing.** `Permission` and `PermissionGroup` are replaced by Scopes, Roles and RoleBindings (`overview.md`). They're kept only so that installations can see what they had and recreate it.

- **No access.** A Permission or PermissionGroup, whether a Kubernetes resource or a row in `permissions` or `permission_groups`, MUST NOT allow or deny anything, in checks, listings or the access summary.
- **Kubernetes resources.** Their controller stores nothing. It sets each one's `Ready` condition to `False` with reason `Deprecated`. Deleting one deletes the row it was stored as, if any; creating or changing one changes nothing.
- **Rows.** Existing rows are kept, so that admins can recreate what they granted as Scopes, Roles and RoleBindings. They can't be created or changed: `/db` refuses those writes with 405. Reading them takes `read` on `roleBinding`, like any other grant; deleting them takes `delete` on `roleBinding`.

**Why.** Few installations use Permissions, and running them next to RoleBindings meant two systems decided access. Upgrading installations recreate theirs as RoleBindings when they upgrade. Rows written through the UI have no manifest anywhere, so they're kept for reference rather than dropped.
