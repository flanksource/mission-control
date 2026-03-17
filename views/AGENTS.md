# Views Package

This package implements the View feature - a Grafana-like dashboard system for Mission Control that allows creating customizable data views with tables, panels, and visualizations.

## Overview

Views are customizable dashboards that can display data from multiple sources including configs, config changes, Prometheus metrics, other view tables, and direct SQL queries.

Views can be created via CRD (`api/v1/view_types.go`) or through the UI.

## Architecture

### Data Flow

1. **Query Execution**: Multiple queries run in parallel (up to 10 concurrent)
2. **SQLite Ingestion**: Results are loaded into an in-memory SQLite database
3. **Panel/Merge Queries**: SQL queries run against the SQLite database
4. **Data Persistence**: Results are cached in PostgreSQL tables for fast retrieval

### Package Structure

| File            | Purpose                                              |
| --------------- | ---------------------------------------------------- |
| `controller.go` | HTTP handlers and route registration                 |
| `table.go`      | View table lifecycle, caching, and persistence       |
| `run.go`        | Core view execution logic (queries, panels, mapping) |
| `scopes.go`     | Scope-based access control (RLS grants)              |

## Key Components

### View Execution (`run.go`)

The `Run()` function is the core execution engine. It handles variable templating, parallel query execution, grant computation, SQLite creation, panel execution, and CEL mapping.

### Query Types

Views support multiple query sources defined in `duty/view/query.go`: direct SQL, configs, config changes, Prometheus, and other view tables.

### Caching System (`table.go`)

- **Cache-Control Headers**: Clients can specify `max-age` and timeout
- **Per-Request Cache**: Different variable combinations get separate cache entries (fingerprint-based)
- **Refresh Timeout**: If refresh takes too long, stale data is returned with error status
- **Singleflight**: Concurrent requests for the same view+variables are deduplicated

### Variables and Templating

Variables filter and parameterize queries. Variables with `dependsOn` are processed using topological sort. See `populateViewVariables()` in `table.go`.

### Column Types

See `duty/view/columns.go` for all supported column types (string, number, datetime, duration, gauge, config_item, labels, health, etc.).

### Panels

Panels are visualizations that query the in-memory SQLite database. See `api/views.go` for panel types (piechart, gauge, bargauge, number, table, timeseries, text, properties, duration).

### Row-Level Security (RLS)

Views implement RLS via the `__grants` column. Each config row is matched against scope selectors, and matching scope IDs are stored for PostgreSQL RLS policy filtering.

- **Scope Caching**: Scope configurations are cached in memory (`scopes.go`). Use `FlushScopeCache()` to invalidate.
- **RLS Context**: Operations needing RLS context use `auth.WithRLS()`.

## HTTP API

Routes are registered in `controller.go`:

- `GET /view/list` - List views for a config
- `GET /view/:id` - Get view by ID
- `GET|POST /view/:namespace/:name` - Get view by namespace/name
- `GET /view/display-plugin-variables/:viewID` - Get plugin variables for config

## Database Schema

- **View Tables**: Each view with columns creates a dedicated table. See `View.TableName()` in `api/v1/view_types.go`.
- **Panel Results**: Stored in `view_panels` table with `request_fingerprint` for per-variable caching.

## Integration with Other Repos

- **duty repo** (`~/projects/flanksource/duty/view/`): Shared view types, column definitions, and database operations
- **UI repo** (`~/projects/flanksource/flanksource-ui`): Frontend rendering of views

## CRD

Definition is in `api/v1/view_types.go`. Validation is handled by kubebuilder annotations and `ViewSpec.Validate()`.

## Testing

```bash
ginkgo -focus "Views" -r
ginkgo -focus "Table" -r
```

Test data is in `testdata/` directory.

## Error Handling

Uses duty's Oops error framework with codes: `EINVALID`, `ENOTFOUND`, `EFORBIDDEN`. Errors during refresh return cached data with error in `refreshError` field.
