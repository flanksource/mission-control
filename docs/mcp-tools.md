# MCP Server Reference

## Resource Templates

| URI Template | Description |
|-------------|-------------|
| `config_item://{id}` | Returns the complete JSON representation of an infrastructure configuration item (AWS EC2 instance, Kubernetes deployment, etc). Use to read the full state of a known item; use `search_catalog` and `describe_catalog` to discover resources. |
| `playbook://{idOrName}` | Returns the JSON definition of an automated runbook including steps and parameters. Use to inspect a playbook's logic; use the dynamic per-session playbook tools to execute it. |
| `connection://{namespace}/{name}` | Returns JSON configuration for an external service endpoint (database, API, cloud provider). Use to inspect a known connection; use `list_connections` to discover available connections. |
| `view://{namespace}/{name}` | Returns the JSON structural definition and query logic of a saved view/dashboard. Use to understand how a view is constructed; use the dynamic view tools to execute the query and fetch data. |

## Prompts

| Name | Description |
|------|-------------|
| `Unhealthy catalog items` | Searches for all unhealthy items using `search_catalog` with query `health!=healthy` |
| `troubleshoot_kubernetes_resource` | Troubleshoots Kubernetes resources. Accepts optional `query` argument (default: `health!=healthy type=Kubernetes::*`) |

## Tools (50 static + dynamic)

| Tool | Hints | Description |
|------|-------|-------------|
| [`add_team_members`](#add_team_members) | mutating | Add people to a team, all or none. |
| [`approve_playbook_run`](#approve_playbook_run) | mutating | Approve a playbook run waiting for approval, as the caller. |
| [`cancel_playbook_run`](#cancel_playbook_run) | destructive | Cancel a playbook run that hasn't finished. |
| [`check_access`](#check_access) | read-only | Check whether subjects may perform an action on a resource, and with explain, which rules decide it. |
| [`create_notification_silence`](#create_notification_silence) | mutating | Create a notification silence, which stops notifications for matching resources between from and until. |
| [`create_rbac_object`](#create_rbac_object) | mutating | Create a Mission Control Scope (a set of resources), Role (rules: an action on a Scope) or RoleBinding (grants a Role to subjects). |
| [`create_team`](#create_team) | mutating | Create a team without members. |
| [`delete_notification_silence`](#delete_notification_silence) | destructive | Delete a notification silence, so the notifications it stopped are sent again. |
| [`delete_rbac_object`](#delete_rbac_object) | destructive | Delete a Mission Control Scope, Role or RoleBinding created through the UI, API or MCP. |
| [`delete_team`](#delete_team) | destructive | Delete a team and remove its members, who lose what the team grants. |
| [`describe_catalog`](#describe_catalog) | read-only | Get all data for configs. |
| [`eval_template`](#eval_template) | read-only | Evaluate a CEL expression or Go template against the provided env map and return the rendered string. |
| [`get_check_status`](#get_check_status) | read-only | Get health check execution history. |
| [`get_job_run`](#get_job_run) | read-only | Get the complete persisted record for a job execution, including its details and errors. |
| [`get_notification_detail`](#get_notification_detail) | read-only | Get one notification send by ID: its status and full error, body, body_payload and body_markdown (rendered body), the original event payload, the recipient (person, team, connection or playbook run), the resource it was sent for, and the notification rule that sent it. |
| [`get_notifications_for_resource`](#get_notifications_for_resource) | read-only | Get notification history for a specific resource (config item, component, check, or canary) with optional time and status filtering. |
| [`get_person`](#get_person) | read-only | Get a Mission Control person by id, e.g. the created_by of an RBAC object, including people who were deleted. |
| [`get_playbook_failed_runs`](#get_playbook_failed_runs) | read-only | Get recent failed playbook runs as JSON array. |
| [`get_playbook_recent_runs`](#get_playbook_recent_runs) | read-only | Get recent playbook execution history as JSON array. |
| [`get_playbook_run_steps`](#get_playbook_run_steps) | read-only | Get detailed information about a playbook run including all actions. |
| [`get_rbac_object`](#get_rbac_object) | read-only | Get one Mission Control Scope, Role or RoleBinding by namespace and name, with whether it's in effect and, for Roles and RoleBindings, which rules don't apply through a binding. |
| [`get_related_configs`](#get_related_configs) | read-only | Find configuration items related to a specific config by relationships and dependencies |
| [`list_all_checks`](#list_all_checks) | read-only | List all health checks with complete metadata including names, IDs, and current status |
| [`list_catalog_types`](#list_catalog_types) | read-only | List all config types |
| [`list_connections`](#list_connections) | read-only | List connections by id, name, namespace, type and source. |
| [`list_job_runs`](#list_job_runs) | read-only | List persisted job execution history, ordered by most recent run first. |
| [`list_jobs`](#list_jobs) | read-only | List jobs scheduled in this Mission Control server, including their cron schedule and next run time. |
| [`list_notification_sends`](#list_notification_sends) | read-only | List recent notification sends across all notifications, newest first, to find failing or silenced notifications. |
| [`list_notification_silences`](#list_notification_silences) | read-only | List notification silences, newest first. |
| [`list_person_teams`](#list_person_teams) | read-only | List the teams a person is a member of, with how each membership was added (source). |
| [`list_rbac_objects`](#list_rbac_objects) | read-only | List Mission Control Scopes, Roles or RoleBindings, with whether each is in effect (error, error_reason). |
| [`list_team_members`](#list_team_members) | read-only | List the members of a team. |
| [`list_teams`](#list_teams) | read-only | List teams with their number of members. |
| [`read_artifact_content`](#read_artifact_content) | read-only | Read the actual content of an artifact file. |
| [`read_artifact_metadata`](#read_artifact_metadata) | read-only | Get artifact metadata by ID including filename, size, content type, path, check/playbook run association, and timestamps |
| [`remove_team_member`](#remove_team_member) | destructive | Remove a person from a team. |
| [`resolve_config`](#resolve_config) | read-only | Resolve a config item by ID or name to get its UUID. |
| [`resolve_external_group`](#resolve_external_group) | read-only | Resolve an external group by ID or name to get their UUID and details. |
| [`resolve_external_user`](#resolve_external_user) | read-only | Resolve an external user by ID, name, or email to get their UUID and details. |
| [`run_health_check`](#run_health_check) | destructive | Execute a health check immediately and return results. |
| [`search_catalog`](#search_catalog) | read-only | Search and find configuration items (not health checks) in the catalog. |
| [`search_catalog_access_log`](#search_catalog_access_log) | read-only | Search historical sign-in and access activity logs for a specific infrastructure configuration item. |
| [`search_catalog_access_mapping`](#search_catalog_access_mapping) | read-only | Search the current access state and RBAC mappings for infrastructure resources to audit who currently holds permissions. |
| [`search_catalog_access_reviews`](#search_catalog_access_reviews) | read-only | Search historical access review and certification events to verify when user permissions were last audited or validated. |
| [`search_catalog_changes`](#search_catalog_changes) | read-only | Search configuration change events globally or for a config and its related configs. |
| [`search_health_checks`](#search_health_checks) | read-only | Search and find health checks returning JSON array with check metadata |
| [`test_connection`](#test_connection) | read-only | Test whether a connection works, by connecting with its credentials. |
| [`update_rbac_object`](#update_rbac_object) | destructive | Replace a Mission Control Scope, Role or RoleBinding created through the UI, API or MCP. |
| [`update_team`](#update_team) | destructive | Rename a team or change its icon. |
| [`whoami`](#whoami) | read-only | Show who the caller is signed in as: their person (id, name, email, type), how they're signed in (oidc: a token of Mission Control's OIDC provider; session: a login session or basic auth; access_token: a personal access token; service_account: a service account's access token), the access token's name, expiry and the role bindings that limit it, the role bindings that grant the caller, and what they may do: the actions they hold and, by resource type and action, whether they may act on all, some or none of it. |
| `{playbook}_{namespace}_{category}` | mutating | Dynamic per-session playbook tools. Parameters derived from playbook spec. |
| `view_{name}_{namespace}` | read-only | Dynamic view tools synced hourly. Returns table rows by default with select/page/limit controls. |

### Dynamic Tools

These tools are registered at runtime and do not appear in the static list above.

**Playbook Tools** — Each playbook is registered as an MCP tool per-session.
Names follow the pattern `{name}_{namespace}_{category}` (e.g. `restart-deployment_mission-control_kubernetes`).
Parameters are derived from each playbook's parameter spec. These tools are **mutating**.

**View Tools** — Each view is registered as an MCP tool, synced every hour.
Names follow the pattern `view_{name}_{namespace}` (e.g. `view_pod-overview_mission-control`).
All view tools are **read-only** and accept these common parameters:

| Name | Type | Default | Description |
|------|------|---------|-------------|
| `withRows` | boolean | true | Include table rows (paginated) |
| `withPanels` | boolean | false | Include panel data |
| `select` | array | all | Columns to include in result |
| `page` | integer | 1 | Page number (1-based) |
| `limit` | integer | 50 | Rows per page (max 500) |

Views may also expose template variables as additional string parameters.

## Tool Details

### `add_team_members`

Add people to a team, all or none. Fails when a person is already a member or can't be a team member.

**Hints:** mutating

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `people` | array | Yes | Emails or UUIDs of the people to add |
| `team` | string | Yes | Name or UUID of the team |

### `approve_playbook_run`

Approve a playbook run waiting for approval, as the caller. The run starts once it has the approvals its playbook requires.

**Hints:** mutating

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `run_id` | string | Yes | UUID of the playbook run |

### `cancel_playbook_run`

Cancel a playbook run that hasn't finished.

**Hints:** destructive

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `run_id` | string | Yes | UUID of the playbook run |

### `check_access`

Check whether subjects may perform an action on a resource, and with explain, which rules decide it. Use it to answer why someone can or can't do something.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `action` | string | Yes | The action, e.g. read, update, playbook:run, playbook:approve, connection:use |
| `explain` | boolean |  | Include the rules that decide each subject's answer |
| `resource` | object | Yes | The resource, by UUID: one of {playbook, config, component, check, view, connection}, or {global: <RBAC object>} to check the action on a whole type, e.g. {global: "catalog"}. |
| `subjects` | array | Yes | People by email or UUID, teams by name or UUID, or ["*"] for every subject the policy grants something to |

### `create_notification_silence`

Create a notification silence, which stops notifications for matching resources between from and until. At least one of config_id, component_id, check_id, canary_id, filter or selectors is required.

**Hints:** mutating

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `canary_id` | string |  | UUID of a canary to silence |
| `check_id` | string |  | UUID of a health check to silence |
| `component_id` | string |  | UUID of a component to silence |
| `config_id` | string |  | UUID of a config item to silence |
| `description` | string |  | Why the notifications are silenced |
| `filter` | string |  | CEL expression a notification's event must match to be silenced |
| `from` | string |  | Start, as datemath, e.g. now. |
| `name` | string | Yes | Name of the silence |
| `recursive` | boolean |  | Also silence the resource's children |
| `selectors` | array |  | Resource selectors of the resources to silence, e.g. [{"types": ["Kubernetes::Pod"], "namespace": "staging"}] |
| `until` | string |  | End, as datemath, e.g. now+4h |

### `create_rbac_object`

Create a Mission Control Scope (a set of resources), Role (rules: an action on a Scope) or RoleBinding (grants a Role to subjects). An object that's invalid on its own is rejected. One whose references are missing or don't fit is stored, and the result says why it isn't in effect. A RoleBinding is what grants access: to grant a permission, reuse a Scope and Role found with list_rbac_objects, create the ones missing, then create the RoleBinding. Objects can be created in any order.

**Hints:** mutating

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `kind` | string | Yes | Kind of object: scopes, roles or role-bindings |
| `object` | object | Yes | The object as the /rbac API and the CRD take it: {metadata: {name, namespace}, spec: {...}}. |

### `create_team`

Create a team without members. Add members with add_team_members.

**Hints:** mutating

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `icon` | string |  | Icon of the team |
| `name` | string | Yes | Name of the team, unique among teams |

### `delete_notification_silence`

Delete a notification silence, so the notifications it stopped are sent again. Silences managed through Kubernetes can't be deleted here.

**Hints:** destructive

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | string | Yes | UUID of the silence, from list_notification_silences |

### `delete_rbac_object`

Delete a Mission Control Scope, Role or RoleBinding created through the UI, API or MCP. Deleting a RoleBinding revokes what it grants.

**Hints:** destructive

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `kind` | string | Yes | Kind of object: scopes, roles or role-bindings |
| `name` | string | Yes | Name of the object |
| `namespace` | string | Yes | Namespace of the object |

### `delete_team`

Delete a team and remove its members, who lose what the team grants. Teams shipped with Mission Control or managed through Kubernetes can't be deleted here.

**Hints:** destructive

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `team` | string | Yes | Name or UUID of the team |

### `describe_catalog`

Get all data for configs. 
	Describe tool returns detailed metadata of a config item.
	Provide a single config item id (UUID) from search_catalog results to fetch the full record.

	Each config item returned will have a field "available_tools", which refers to all the existing tools in the current mcp server.
	We can call those tools with the param config_id=<id> and ask the user for any other parameters if the input schema requires any.

	NOTE: This tool is explicitly for config items and not for health checks.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | string | Yes | Config item id (UUID) |

### `eval_template`

Evaluate a CEL expression or Go template against the provided env map and return the rendered string. Provide exactly one of cel_expression or gotemplate.For the list of available cel and template functions: Visit https://flanksource.com/docs/reference/scripting/cel.md

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `cel_expression` | string |  | CEL expression to evaluate against env. |
| `change_id` | string |  | Optional catalog change UUID to fetch and inject into env as `change`. |
| `check_id` | string |  | Optional check UUID to fetch and inject into env as `check`. |
| `config_id` | string |  | Optional config item UUID to fetch and inject into env as `config`. |
| `env` | object |  | Environment map available to the expression/template. |
| `gotemplate` | string |  | Go text/template to render using env. |

### `get_check_status`

Get health check execution history. Each entry contains status, time, duration, and error (if any). Ordered by most recent first.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | string | Yes | Health check ID to get status history for |
| `limit` | number |  | Number of status entries to return (default: 30) |

### `get_job_run`

Get the complete persisted record for a job execution, including its details and errors.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | string | Yes | Job run ID. |

### `get_notification_detail`

Get one notification send by ID: its status and full error, body, body_payload and body_markdown (rendered body), the original event payload, the recipient (person, team, connection or playbook run), the resource it was sent for, and the notification rule that sent it. Use list_notification_sends to find send IDs.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `send_id` | string | Yes | UUID of the notification send history record |

### `get_notifications_for_resource`

Get notification history for a specific resource (config item, component, check, or canary) with optional time and status filtering. Returns minimal fields by default to save tokens. Use get_notification_detail to get full notification details.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `before` | string |  | End time filter using datemath expressions (e.g., 'now-1h', 'now-1d') |
| `limit` | number |  | Maximum number of notifications to return (default: 10) |
| `resource_id` | string | Yes | UUID of the resource (from catalog, component, or health check) |
| `select` | array |  | Array of field names to return (default: ["id","status","count","resource_health","resource_status","resource_health_description","first_observed","source_event","created_at","resource_id","notification_id"]) |
| `since` | string |  | Start time filter using datemath expressions (e.g., 'now-24h', 'now-7d', 'now-30m') |
| `status` | string |  | Filter by notification status (sending, sent, error, pending, silenced, repeat-interval, inhibited, pending_playbook_run, pending_playbook_completion, evaluating-waitfor, attempting_fallback) |

### `get_person`

Get a Mission Control person by id, e.g. the created_by of an RBAC object, including people who were deleted.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | string | Yes | UUID of the person |

### `get_playbook_failed_runs`

Get recent failed playbook runs as JSON array. Each entry contains failure details, error messages, and timing information.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | number |  | Maximum number of failed runs to return (default: 20) |
| `playbook_id` | string |  | Optional UUID of the playbook to filter runs by. |

### `get_playbook_recent_runs`

Get recent playbook execution history as JSON array. Each entry contains run details, status, timing, and results.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | number |  | Maximum number of recent runs to return (default: 20) |
| `playbook_id` | string |  | Optional UUID of the playbook to filter runs by. |

### `get_playbook_run_steps`

Get detailed information about a playbook run including all actions. Returns actions from both the run and any child runs. Actions are ordered by start time.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `run_id` | string | Yes | The UUID of the playbook run to get details for |
| `withResult` | boolean |  | Include the result field from actions. |

### `get_rbac_object`

Get one Mission Control Scope, Role or RoleBinding by namespace and name, with whether it's in effect and, for Roles and RoleBindings, which rules don't apply through a binding.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `kind` | string | Yes | Kind of object: scopes, roles or role-bindings |
| `name` | string | Yes | Name of the object |
| `namespace` | string | Yes | Namespace of the object |

### `get_related_configs`

Find configuration items related to a specific config by relationships and dependencies

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | string | Yes | Config ID |

### `list_all_checks`

List all health checks with complete metadata including names, IDs, and current status

**Hints:** read-only

### `list_catalog_types`

List all config types

**Hints:** read-only

### `list_connections`

List connections by id, name, namespace, type and source. Settings and credentials aren't returned. Returns empty array if no connections configured. Use for discovering available data sources.

**Hints:** read-only

### `list_job_runs`

List persisted job execution history, ordered by most recent run first. Returns compact diagnostics; use get_job_run for complete details and errors.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `agent_id` | string |  | Optional exact agent ID filter. |
| `before` | string |  | Optional inclusive end time in date-math or RFC3339 format, e.g. now-1h. |
| `has_errors` | boolean |  | Optional filter for runs with (true) or without (false) errors. |
| `limit` | number |  | Maximum runs to return (default: 20, maximum: 100). |
| `name` | string |  | Optional case-insensitive partial match against the job name. |
| `resource_id` | string |  | Optional exact resource ID filter. |
| `resource_type` | string |  | Optional exact resource type filter. |
| `since` | string |  | Optional inclusive start time in date-math or RFC3339 format, e.g. now-24h. |
| `status` | array |  | Optional statuses: RUNNING, SUCCESS, WARNING, FAILED, STALE, SKIPPED. |

### `list_jobs`

List jobs scheduled in this Mission Control server, including their cron schedule and next run time. Use list_job_runs for execution history.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | number |  | Maximum jobs to return (default: 50, maximum: 100). |
| `name` | string |  | Optional case-insensitive partial match against the job name or aliases. |
| `resource_id` | string |  | Optional exact resource ID filter. |
| `resource_type` | string |  | Optional exact resource type filter. |

### `list_notification_sends`

List recent notification sends across all notifications, newest first, to find failing or silenced notifications. Each row has the send's id, time, status, event, resource, notification name and namespace, recipient and error. Use get_notification_detail with a row's id for its body and full details.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `before` | string |  | End time filter using datemath expressions (e.g., 'now-1h', 'now-1d') |
| `connection` | string |  | Recipient connection ID, 'namespace/name', or name |
| `limit` | number |  | Maximum number of sends to return, 1 to 200 (default: 20) |
| `notification` | string |  | Notification ID, 'namespace/name', or name |
| `resource_id` | string |  | UUID of the resource (config item, component, check or canary) the notification was sent for |
| `since` | string |  | Start time filter using datemath expressions (e.g., 'now-24h', 'now-7d', 'now-30m') |
| `status` | string |  | Comma-separated statuses to include, e.g. 'error' or 'error,attempting_fallback' (sending, sent, error, pending, skipped, silenced, repeat-interval, inhibited, pending_playbook_run, pending_playbook_completion, evaluating-waitfor, attempting_fallback) |

### `list_notification_silences`

List notification silences, newest first. Expired silences are left out unless include_expired is set.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `include_expired` | boolean |  | Also list silences whose until has passed |
| `search` | string |  | Only list silences whose name or description contains this, case-insensitive |

### `list_person_teams`

List the teams a person is a member of, with how each membership was added (source).

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `person` | string | Yes | Email or UUID of the person |

### `list_rbac_objects`

List Mission Control Scopes, Roles or RoleBindings, with whether each is in effect (error, error_reason).

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `kind` | string | Yes | Kind of object: scopes, roles or role-bindings |
| `namespace` | string |  | Only list objects in this namespace. |

### `list_team_members`

List the members of a team. Each member's source says how they were added: members whose source is team-mapper or starts with provider/ are managed by a team mapper or an identity provider and can't be removed with remove_team_member.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `team` | string | Yes | Name or UUID of the team |

### `list_teams`

List teams with their number of members.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `search` | string |  | Only list teams whose name contains this, case-insensitive |

### `read_artifact_content`

Read the actual content of an artifact file. Content will be truncated if it exceeds max_length.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | string | Yes | UUID of the artifact |
| `max_length` | number |  | Maximum number of bytes to read from the artifact. |

### `read_artifact_metadata`

Get artifact metadata by ID including filename, size, content type, path, check/playbook run association, and timestamps

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | string | Yes | UUID of the artifact |

### `remove_team_member`

Remove a person from a team. Only members added through the UI, API or MCP can be removed; memberships a team mapper or an identity provider added are left to them.

**Hints:** destructive

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `person` | string | Yes | Email or UUID of the person |
| `team` | string | Yes | Name or UUID of the team |

### `resolve_config`

Resolve a config item by ID or name to get its UUID. Use this when you have a config name and need its ID for other tools like search_catalog_access_log or describe_catalog.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | number |  | Max results to return (default: 10) |
| `query` | string | Yes | Config item UUID or name to search for. |
| `type` | string |  | Optional config type filter (e.g. 'Kubernetes::Deployment', 'AWS::EC2::Instance') |

### `resolve_external_group`

Resolve an external group by ID or name to get their UUID and details. Use this to look up group identifiers for access audit queries.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | number |  | Max results to return (default: 10) |
| `query` | string | Yes | Group UUID or name to search for. |

### `resolve_external_user`

Resolve an external user by ID, name, or email to get their UUID and details. Use this to look up user identifiers before calling access audit tools like search_catalog_access_log.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | number |  | Max results to return (default: 10) |
| `query` | string | Yes | User UUID, name, or email to search for. |

### `run_health_check`

Execute a health check immediately and return results. Returns execution status and timing information.

**Hints:** destructive

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | string | Yes | Health check ID to run |

### `search_catalog`

Search and find configuration items (not health checks) in the catalog. For detailed config data, use describe_catalog tool. 
	Each catalog item also has more information in its config field which can be retrieved by calling a different tool: describe_catalog(id).
	Use the id from search results; describe_catalog only accepts a single config id and should be called when "describe" is explicitly used.
	For cross-agent searches, explicitly include agent=all in the query.

	IMPORTANT - Column Selection for Token Efficiency:
	ALWAYS specify the "select" parameter with only the columns you need to minimize token usage.
	Default columns (id,name,type,health,status,description,updated_at,created_at) provide essential metadata.

	Available columns for ConfigItemSummary:
	- Lightweight: id, name, type, status, health, description, created_at, updated_at, deleted_at, scraper_id, agent_id, external_id, source, path, ready, cost_per_minute, cost_total_1d, cost_total_7d, cost_total_30d, delete_reason, labels, tags, namespace, changes, analysis, created_by
	- Note: ConfigItemSummary does NOT include config or properties fields. For full config data, use describe_catalog tool.

	Examples:
	- For basic listing: "id,name,type,health,status"
	- For troubleshooting: "id,name,type,health,status,description,changes"
	- For cost analysis: "id,name,type,cost_per_minute,cost_total_30d"

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | number |  | Number of items to return. |
| `query` | string | Yes | Search query. |
| `select` | array |  | a list of columns to return. |

### `search_catalog_access_log`

Search historical sign-in and access activity logs for a specific infrastructure configuration item. Investigate security events and answer questions like 'when was this resource last accessed?', 'who has been logging into this system?', or 'was MFA used during access?'. Returns a chronological log with user name/email, MFA status, total access count, and activity timestamp.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `config_id` | string | Yes | Config item ID (UUID) |
| `limit` | number |  | Max results to return (default: 50) |
| `user_id` | string |  | External user ID (UUID) to filter logs by. |

### `search_catalog_access_mapping`

Search the current access state and RBAC mappings for infrastructure resources to audit who currently holds permissions. Accepts a flexible search query (e.g. type=Kubernetes::*, name=my-app) to answer questions like 'who has access to this app?', 'list all users with access to Kubernetes deployments', or 'find stale access and overdue reviews'. Returns config item name/type, user email/type, assigned role, group name, and timestamps for last sign-in and last review.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | number |  | Max results to return (default: 30) |
| `query` | string | Yes | Search query to filter config items by name or type (e.g. 'type=Kubernetes::*', 'name=my-app') |

### `search_catalog_access_reviews`

Search historical access review and certification events to verify when user permissions were last audited or validated. Answers compliance questions like 'when was access to this resource last reviewed?', 'which resources haven't been reviewed recently?', or 'who performed the last access review?'. Returns config item details, reviewed user/role, review source, and certification timestamp.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `config_id` | string |  | Config item ID (UUID). |
| `limit` | number |  | Max results to return (default: 50) |
| `since` | string |  | How far back to search. |

### `search_catalog_changes`

Search configuration change events globally or for a config and its related configs.
	Related config changes:
	- For a global search, provide query.
	- To fetch changes for one config, omit query and provide config_id.
	- Set related to downstream, upstream, or all to include related configs. The default is none.
	- Set soft=true to include soft relationships during related traversal. The default is false.
	- depth controls the maximum relationship traversal depth and defaults to 5.

	IMPORTANT - Column Selection for Token Efficiency:
	ALWAYS specify the "select" parameter with only the columns you need to minimize token usage.
	Default columns (id,config_id,name,type,change_type,severity,summary,created_at,first_observed,count) provide essential change metadata.

	Available columns for CatalogChange:
	- Lightweight: id, config_id, name, type, change_type, severity, summary, created_at, first_observed, count, external_created_by, created_by, source, deleted_at, agent_id, tags
	- Heavy (avoid unless needed): config, details, diff - these are large JSON fields containing full configuration data, change details, and diffs
	- Config-scoped searches return lightweight columns only.

	Examples:
	- For basic change listing: "id,config_id,name,type,change_type,severity,created_at"
	- For change analysis: "id,config_id,change_type,severity,summary,first_observed,count"
	- For critical changes: "id,config_id,name,change_type,severity,summary,source"
	- Only when full details needed: "id,config_id,change_type,severity,summary,details"
	- For downstream changes including soft relationships: config_id=<uuid>, related=downstream, soft=true, depth=5

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `config_id` | string |  | Config UUID whose changes should be returned. |
| `depth` | integer |  | Maximum related config traversal depth. |
| `limit` | number |  | Number of results to return. |
| `query` | string |  | Global search query. |
| `related` | string |  | Related config direction. |
| `select` | array |  | a list of columns to return. |
| `soft` | boolean |  | Include soft relationships in related config traversal. |

### `search_health_checks`

Search and find health checks returning JSON array with check metadata

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | number |  | Number of items to return |
| `query` | string | Yes | Search query. |

### `test_connection`

Test whether a connection works, by connecting with its credentials. Returns only whether it works and the error if it doesn't.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `connection` | string | Yes | UUID or namespace/name of the connection |

### `update_rbac_object`

Replace a Mission Control Scope, Role or RoleBinding created through the UI, API or MCP. Objects managed through Kubernetes can't be changed here.

**Hints:** destructive

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `kind` | string | Yes | Kind of object: scopes, roles or role-bindings |
| `name` | string | Yes | Name of the object |
| `namespace` | string | Yes | Namespace of the object |
| `object` | object | Yes | The object as the /rbac API and the CRD take it: {metadata: {name, namespace}, spec: {...}}. |

### `update_team`

Rename a team or change its icon. Teams shipped with Mission Control or managed through Kubernetes can't be changed here. Renaming a team changes which RoleBindings select it, since they select teams by name.

**Hints:** destructive

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `icon` | string |  | New icon of the team |
| `name` | string |  | New name of the team |
| `team` | string | Yes | Name or UUID of the team |

### `whoami`

Show who the caller is signed in as: their person (id, name, email, type), how they're signed in (oidc: a token of Mission Control's OIDC provider; session: a login session or basic auth; access_token: a personal access token; service_account: a service account's access token), the access token's name, expiry and the role bindings that limit it, the role bindings that grant the caller, and what they may do: the actions they hold and, by resource type and action, whether they may act on all, some or none of it.

**Hints:** read-only

