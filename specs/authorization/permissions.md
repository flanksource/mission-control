# Permissions

**Deprecated.** `Permission` and `PermissionGroup` are replaced by Scopes, Roles and RoleBindings (`overview.md`), and will be removed.

- **Supported, but frozen.** Permissions keep working until they're removed, and get no new features.
- **Roles and RoleBindings come first.** No requirement of Scopes, Roles or RoleBindings is weakened, and no design of them made harder, to keep a Permission working as it does. Where the two conflict, the Permission changes or loses the feature, with no compatibility path.
- **Never wider.** Such a change MAY make a Permission grant less, or nothing. It MUST NOT make one grant more: where a deny Permission can no longer be enforced, what it denied is refused instead.
- **Not specified here.** These specifications don't define how Permissions behave, and where one mentions Permissions, the mention isn't a requirement. A design that changes how Permissions behave says so.

**Why.** Few installations use Permissions. Keeping every behaviour of theirs would constrain the system that replaces them, for little gain. But someone relying on a deny must never find it quietly allowing.
