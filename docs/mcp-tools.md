# MCP Server Reference

## Resource Templates

| URI Template | Description |
|-------------|-------------|
| `config_item://{id}` | Returns the complete JSON representation of an infrastructure configuration item (AWS EC2 instance, Kubernetes deployment, etc). Use to read the full state of a known item; use `search_catalog` and `describe_catalog` to discover resources. Example: `config_item://i-0abcd1234efgh5678` |
| `playbook://{idOrName}` | Returns the JSON definition of an automated runbook including steps and parameters. Use to inspect a playbook's logic; use the dynamic per-session playbook tools to execute it. Example: `playbook://restart-k8s-pods` |
| `connection://{namespace}/{name}` | Returns JSON configuration for an external service endpoint (database, API, cloud provider). Use to inspect a known connection; use `list_connections` to discover available connections. Example: `connection://default/postgres-main` |
| `view://{namespace}/{name}` | Returns the JSON structural definition and query logic of a saved view/dashboard. Use to understand how a view is constructed; use the dynamic view tools to execute the query and fetch data. Example: `view://monitoring/high-cpu-instances` |

## Prompts

| Name | Description |
|------|-------------|
| `Unhealthy catalog items` | Searches for all unhealthy items using `search_catalog` with query `health!=healthy` |
| `troubleshoot_kubernetes_resource` | Troubleshoots Kubernetes resources. Accepts optional `query` argument (default: `health!=healthy type=Kubernetes::*`) |

## Tools (21 static + dynamic)

| Tool | Hints | Description |
|------|-------|-------------|
| [`describe_catalog`](#describe_catalog) | read-only | Get all data for configs. |
| [`eval_template`](#eval_template) | read-only | Evaluate a CEL expression or Go template against the provided env map and return the rendered string. |
| [`get_check_status`](#get_check_status) | read-only | Get health check execution history. |
| [`get_notification_detail`](#get_notification_detail) | read-only | Get detailed information about a specific notification including status, body_markdown (rendered body), recipients, resource details, and related entities |
| [`get_notifications_for_resource`](#get_notifications_for_resource) | read-only | Get notification history for a specific resource (config item, component, check, or canary) with optional time and status filtering. |
| [`get_playbook_failed_runs`](#get_playbook_failed_runs) | read-only | Get recent failed playbook runs as JSON array. |
| [`get_playbook_recent_runs`](#get_playbook_recent_runs) | read-only | Get recent playbook execution history as JSON array. |
| [`get_playbook_run_steps`](#get_playbook_run_steps) | read-only | Get detailed information about a playbook run including all actions. |
| [`get_related_configs`](#get_related_configs) | read-only | Find configuration items related to a specific config by relationships and dependencies |
| [`list_all_checks`](#list_all_checks) | read-only | List all health checks with complete metadata including names, IDs, and current status |
| [`list_catalog_types`](#list_catalog_types) | read-only | List all config types |
| [`list_connections`](#list_connections) | read-only | List all connection endpoints and credentials. |
| [`read_artifact_content`](#read_artifact_content) | read-only | Read the actual content of an artifact file. |
| [`read_artifact_metadata`](#read_artifact_metadata) | read-only | Get artifact metadata by ID including filename, size, content type, path, check/playbook run association, and timestamps |
| [`run_health_check`](#run_health_check) | destructive | Execute a health check immediately and return results. |
| [`search_catalog`](#search_catalog) | read-only | Search and find configuration items (not health checks) in the catalog. |
| [`search_catalog_access_log`](#search_catalog_access_log) | read-only | Search historical sign-in and access activity logs for a specific infrastructure configuration item. |
| [`search_catalog_access_mapping`](#search_catalog_access_mapping) | read-only | Search the current access state and RBAC mappings for infrastructure resources to audit who currently holds permissions. |
| [`search_catalog_access_reviews`](#search_catalog_access_reviews) | read-only | Search historical access review and certification events to verify when user permissions were last audited or validated. |
| [`search_catalog_changes`](#search_catalog_changes) | read-only | Search and find configuration change events across catalog items. |
| [`search_health_checks`](#search_health_checks) | read-only | Search and find health checks returning JSON array with check metadata |
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

### `get_notification_detail`

Get detailed information about a specific notification including status, body_markdown (rendered body), recipients, resource details, and related entities

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
| `status` | string |  | Filter by notification status (sent, error, pending, silenced, repeat-interval, inhibited, pending_playbook_run, pending_playbook_completion, evaluating-waitfor, attempting_fallback) |

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

List all connection endpoints and credentials. Returns empty array if no connections configured. Use for discovering available data sources.

**Hints:** read-only

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

### `search_catalog_access_mapping`

Search the current access state and RBAC mappings for infrastructure resources to audit who currently holds permissions. Accepts a flexible search query (e.g. type=Kubernetes::*, name=my-app) to answer questions like 'who has access to this app?', 'list all users with access to Kubernetes deployments', or 'find stale access and overdue reviews'. Returns config item name/type, user email/type, assigned role, group name, and timestamps for last sign-in and last review.

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | number |  | Max results to return (default: 30) |
| `query` | string | Yes | Search query to filter config items by name or type (e.g. |

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

Search and find configuration change events across catalog items.
	IMPORTANT - Column Selection for Token Efficiency:
	ALWAYS specify the "select" parameter with only the columns you need to minimize token usage.
	Default columns (id,config_id,name,type,change_type,severity,summary,created_at,first_observed,count) provide essential change metadata.

	Available columns for CatalogChange:
	- Lightweight: id, config_id, name, type, change_type, severity, summary, created_at, first_observed, count, external_created_by, created_by, source, deleted_at, agent_id, tags
	- Heavy (avoid unless needed): config, details, diff - these are large JSON fields containing full configuration data, change details, and diffs

	Examples:
	- For basic change listing: "id,config_id,name,type,change_type,severity,created_at"
	- For change analysis: "id,config_id,change_type,severity,summary,first_observed,count"
	- For critical changes: "id,config_id,name,change_type,severity,summary,source"
	- Only when full details needed: "id,config_id,change_type,severity,summary,details"

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | number |  | Number of results to return. |
| `query` | string | Yes | Search query We can search all the catalog changes via query Use the tool: list_catalog_types to get all the types first to make inference is better (cache them for 15m) FORMAL PEG GRAMMAR: Query = AndQuery _ OrQuery* OrQuery = _ '|' _ AndQuery AndQuery = _ FieldQuery _ FieldQuery* FieldQuery = _ '(' _ Query _ ')' _ / _ Field _ / _ '-' Word _ / _ (Word / Identifier) _ Field = Source _ Operator _ Value Source = Identifier ('.' Identifier)* Operator = "<=" / ">=" / "=" / ":" / "!=" / "<" / ">" Value = DateTime / ISODate / Time / Measure / Float / Integer / Identifier / String String = '"' [^"]* '"' ISODate = [0-9]{4} '-' [0-9]{2} '-' [0-9]{2} Time = [0-2][0-9] ':' [0-5][0-9] ':' [0-5][0-9] DateTime = "now" (("+" / "-") Integer DurationUnit)? / ISODate ? Time? DurationUnit = "s" / "m" / "h" / "d" / "w" / "mo" / "y" Word = String / '-'? [@a-zA-Z0-9-]+ Integer = [+-]?[0-9]+ ![a-zA-Z0-9_-] Float = [+-]? [0-9] '.' [0-9]+ Measure = (Integer / Float) Identifier Identifier = [@a-zA-Z0-9_\,-:\[\]]+ _ = [ \t] EOF = !. |
| `select` | array |  | a list of columns to return. |

### `search_health_checks`

Search and find health checks returning JSON array with check metadata

**Hints:** read-only

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | number |  | Number of items to return |
| `query` | string | Yes | Search query. |

