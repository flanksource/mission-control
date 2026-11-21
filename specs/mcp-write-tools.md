# MCP Write Tools

Mission Control's MCP server lets an AI agent create and change Mission Control resources, not just read them. This spec covers the first set: Scopes, Roles and RoleBindings, a single tool that grants one permission, and notification silences.

The terms **MUST**, **MUST NOT** and **MAY** describe requirements.

## 1. Rules every write tool follows

### 1.1 Same checks as the API

A write tool MUST make exactly the checks the API route for the same write makes, as the caller, limited by the caller's access token (`authorization/limiting-permissions.md`). It MUST NOT check anything else, or anything less.

_Why:_ an agent acts for its caller. A tool that checked less would be a way around the API; one that checked more, or differently, would make the same grant mean different things in two places.

Reaching any tool also needs `mcp:use` (`authorization/roles.md`, Section 2.3). An access token made for an agent therefore needs both `mission-control-builtin/mcp-user` and Roles allowing the writes, e.g. `mission-control-builtin/access-manage`.

### 1.2 Same validation as the API

A write tool takes an object in the shape the API takes, and validates it the same way: an object that's wrong on its own is rejected and nothing is stored, unknown fields included (`authorization/overview.md`, "API and Kubernetes validation"). An object that's only invalid because of what it references is stored, and the result says why it isn't in effect.

_Why:_ agents make mistakes. Rejecting what can never work, and reporting what doesn't work yet, gives the agent what it needs to correct itself.

### 1.3 Writes through MCP are API writes

An object written through MCP is stored exactly as if the caller had written it through the API: same source, same `created_by`. It can be changed or deleted through the UI, the API or MCP alike, and objects managed through Kubernetes can't be changed through MCP.

_Why:_ MCP is another client of the API, not a separate place objects live. A different source would split one kind of object into two that are edited in different places.

### 1.4 Results

A successful write returns the stored object as the API's response does, including why it isn't in effect when it isn't, e.g. a Role's `error` and `error_reason`, or a RoleBinding's `unapplied_rules`. A failed write returns an error naming the cause, and stores nothing.

### 1.5 Tool annotations

Every write tool MUST NOT carry the read-only hint. A tool that deletes MUST carry the destructive hint.

_Why:_ MCP clients use the hints to decide what to confirm with the user. Deleting an RBAC object can take away someone's access.

## 2. Scopes, Roles and RoleBindings

`kind` is one of `scopes`, `roles` or `role-bindings`, as in the API's paths (`authorization/overview.md`, "API addresses").

| Tool                 | Input                                   | Does the same as                       | Check                  |
| -------------------- | --------------------------------------- | -------------------------------------- | ---------------------- |
| `list_rbac_objects`  | `kind`, optional `namespace`            | `GET /rbac/<kind>`                     | `read` on the kind     |
| `get_rbac_object`    | `kind`, `namespace`, `name`             | `GET /rbac/<kind>/<namespace>/<name>`  | `read` on the kind     |
| `create_rbac_object` | `kind`, `object`                        | `POST /rbac/<kind>`                    | `create` on the kind   |
| `update_rbac_object` | `kind`, `namespace`, `name`, `object`   | `PUT /rbac/<kind>/<namespace>/<name>`  | `update` on the kind   |
| `delete_rbac_object` | `kind`, `namespace`, `name`             | `DELETE /rbac/<kind>/<namespace>/<name>` | `delete` on the kind |

`object` is the object as the API takes it: `metadata` and `spec`, in the CRD's shape. The reserved namespaces `mission-control-builtin` and `mission-control-access-tokens` are refused for writes, as the API refuses them.

`list_rbac_objects` leaves out access tokens' RoleBindings unless `namespace` is `mission-control-access-tokens`, as the API does.

_Why generic tools over one per kind:_ the three kinds share every rule above, and an agent needs one way to address them, the same as the API's.

## 3. Granting one permission

The commonest request, e.g. "let alice run the `restart-pod` playbook on staging configs", needs a Scope for each side of the rule, a Role and a RoleBinding. `grant_access` writes them in one call.

| Input              | Required                    | Meaning                                                                                              |
| ------------------ | --------------------------- | ---------------------------------------------------------------------------------------------------- |
| `name`             | Yes                         | Name of the Role and RoleBinding it creates, and the prefix of any Scope it creates.                  |
| `namespace`        | Yes                         | Namespace every object it creates is in.                                                             |
| `action`           | Yes                         | The rule's action (`authorization/roles.md`, Section 2).                                             |
| `resource_scope`   | Exactly one of the two, unless the action takes no resource | An existing Scope, as a reference (`authorization/overview.md`, "References").   |
| `resource_targets` | Exactly one of the two, unless the action takes no resource | Targets of a new Scope `<name>-resource` (`authorization/scopes.md`, Section 3). |
| `target_scope`     | At most one of the two      | An existing Scope for the rule's target.                                                             |
| `target_targets`   | At most one of the two      | Targets of a new Scope `<name>-target`.                                                              |
| `subjects`         | Yes                         | The RoleBinding's subjects (`authorization/rolebindings.md`, Section 2).                             |
| `description`      | No                          | Set on every object it creates.                                                                      |

It creates, in `namespace`, the Scopes asked for, a Role `<name>` with one rule `<name>`, and a RoleBinding `<name>` of that Role to `subjects`. It returns every object it created, as in Section 1.4.

- It MUST check `create` on each kind it creates, and MUST refuse before writing anything when any check fails.
- It MUST write all the objects or none. When any of them is rejected, or any of them already exists, nothing is written.
  _Why:_ a half-written grant leaves objects behind that grant nothing and that the agent has to find and clean up.
- It MUST grant exactly the one rule asked for. It MUST NOT add rules the operation also checks, e.g. `read` on the configs a playbook runs on (`authorization/roles.md`, Section 4.3).
  _Why:_ what's written is what's granted. The agent asks for `read` with a second call when the subjects don't have it already.
- It never denies: the RoleBinding it writes has no `deny` and no `constraint`.
  _Why:_ a deny binding is a guardrail for what must stay refused, which deserves a deliberate RoleBinding, not a shortcut.
- The objects it writes are ordinary objects (Section 1.3). Revoking the grant is deleting its RoleBinding with `delete_rbac_object`; the Role and Scopes MAY be deleted too, and grant nothing on their own.

## 4. Notification silences

| Tool                          | Does the same as             | Check                                 |
| ----------------------------- | ---------------------------- | ------------------------------------- |
| `create_notification_silence` | `POST /notification/silence` | `create` on `notification-silences`   |

Its input is the API's: `name`, `description`, `from`, `until`, `recursive`, `filter`, `selectors`, and the ids of a config, component, check or canary. At least one of an id, `filter` or `selectors` is required. `from` and `until` take datemath, e.g. `now` and `now+4h`. It returns the stored silence.

## 5. Not covered yet

- **Notifications.** Notifications are written through the database API, not a route that validates a notification spec. A tool to create them comes with such a route.
- **Updating and deleting notification silences.** The API only creates them.
- **Other resources**, e.g. playbooks, views and connections. They follow Section 1 when they're added.
