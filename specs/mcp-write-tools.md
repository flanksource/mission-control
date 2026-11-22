# MCP Write Tools

Mission Control's MCP server lets an AI agent create and change Scopes, Roles, RoleBindings and notification silences, not just read them.

The terms **MUST**, **MUST NOT** and **MAY** describe requirements.

## 1. Same as the API

Each tool MUST make the same checks and validation as the matching API route, as the caller, and store what it writes exactly as that route does.

_Why:_ an agent acts for its caller, and MCP is just another client of the API. Anything else would make the same grant mean different things in two places.

Every tool also needs `mcp:use` (`authorization/roles.md`, Section 2.3). A tool that deletes or replaces an object carries MCP's destructive hint, so clients can confirm it with the user.

## 2. Scopes, Roles and RoleBindings

`kind` is `scopes`, `roles` or `role-bindings`, and `object` is the object as the API takes it (`authorization/overview.md`, "API addresses").

| Tool                 | Input                                 | Same as                                  |
| -------------------- | ------------------------------------- | ---------------------------------------- |
| `list_rbac_objects`  | `kind`, optional `namespace`          | `GET /rbac/<kind>`                       |
| `get_rbac_object`    | `kind`, `namespace`, `name`           | `GET /rbac/<kind>/<namespace>/<name>`    |
| `create_rbac_object` | `kind`, `object`                      | `POST /rbac/<kind>`                      |
| `update_rbac_object` | `kind`, `namespace`, `name`, `object` | `PUT /rbac/<kind>/<namespace>/<name>`    |
| `delete_rbac_object` | `kind`, `namespace`, `name`           | `DELETE /rbac/<kind>/<namespace>/<name>` |

### 2.1 Granting a permission

The agent grants a permission with these tools alone: it reuses a Scope or Role it finds with `list_rbac_objects`, creates the ones that are missing, and creates the RoleBinding. Mission Control MUST NOT offer a tool that writes several objects for one grant.

_Why:_ the agent can then build any grant the API allows, and Mission Control holds no logic for how grants are composed. A combined tool would limit grants to the shapes it supports.

The calls aren't one transaction. A failed call leaves the objects created before it, which the agent can reuse or delete. Order doesn't matter: an object whose reference is missing is stored `Ready=False` and takes effect once the reference exists (`authorization/overview.md`, "Recovery after dependency changes").

_Why that's acceptable:_ only a RoleBinding grants anything, so a Scope or Role left behind changes no one's access.

For example, "let Alice read the configs in namespace `staging`", with no matching Scope or Role yet:

1. `create_rbac_object`, `kind: scopes`: `default/staging-configs`, target `config: {namespace: staging}`.
2. `create_rbac_object`, `kind: roles`: `default/staging-config-reader`, rule `read` on `staging-configs`.
3. `create_rbac_object`, `kind: role-bindings`: `default/alice-staging-config-reader`, Role `staging-config-reader`, `people: [alice@example.com]`.

The result of each call shows whether the object is in effect, e.g. a Role's `error_reason`, or a RoleBinding's `unapplied_rules`.

## 3. Notification silences

| Tool                          | Same as                      |
| ----------------------------- | ---------------------------- |
| `create_notification_silence` | `POST /notification/silence` |

## 4. Not covered yet

- **Notifications.** They're written through the database API, which doesn't validate a notification spec. A tool comes with a route that does.
- **Updating and deleting notification silences.** The API only creates them.
- **Other resources**, e.g. playbooks, views and connections. They follow Section 1 when they're added.
