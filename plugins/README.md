# Mission Control Plugins

This directory contains the first-party plugins shipped with mission-control,
and the guide for writing new ones. The plugin framework itself lives in
[`plugin/`](../plugin) — proto definitions, SDK, supervisor, and host-side
controller.

A mission-control plugin is an **out-of-process binary**. The host launches it
with a magic-cookie env var, completes a [`go-plugin`][go-plugin] handshake,
and then communicates over gRPC. Plugins serve their own HTTP listener for
UI assets and `/api/*` calls; the host reverse-proxies those at
`/api/plugins/<name>/ui/*`.

## Quickstart

There is no skeleton template — start from a worked example:

| Reference | Use as |
|---|---|
| [`golang/`](golang/) | Full-featured plugin: embedded UI, sessions, profile collection, `HostClient` usage |
| [`kubernetes-logs/`](kubernetes-logs/) | Minimal plugin: operations + streaming HTTP, no host callbacks |
| [`golang/Plugin.yaml`](golang/Plugin.yaml) | Plugin CRD: selector + connection allowlist |

Build with `make dev` from the repo root (never `go build` directly — see the
top-level `AGENTS.md`). The supervisor watches the binary on disk and restarts
the plugin when it changes.

## Lifecycle

```
host launches binary
  → handshake (magic cookie + protocol version)
  → host opens reverse-channel broker
  → RegisterPlugin            (plugin returns manifest, ui_port)
  → Configure                 (host pushes CRD spec.properties)
  → ListOperations            (refresh after Configure if needed)
  ↺ Health (periodic)         + Invoke (on user action)
  → Shutdown
```

The supervisor ([`plugin/supervisor/supervisor.go`](../plugin/supervisor/supervisor.go))
gives the plugin **30 seconds** to complete `RegisterPlugin` and budgets
**10 restarts/hour** before backing off.

## The `Plugin` interface

Plugin authors implement four methods, defined in
[`plugin/sdk/sdk.go`](../plugin/sdk/sdk.go):

```go
type Plugin interface {
    Manifest() *pluginpb.PluginManifest
    Configure(ctx context.Context, settings map[string]any) error
    Operations() []Operation
    HTTPHandler() http.Handler
}
```

- `Manifest()` — static name/version/description, declared `tabs` (frontend
  attaches them to matching catalog items), and the operations the plugin
  exposes. Called once on startup in response to `RegisterPlugin`.
- `Configure()` — applies CRD `spec.properties` (already JSON-decoded into
  `map[string]any`). May be called multiple times if the CRD changes.
- `Operations()` — returns runtime handlers for each declared operation. The
  `Def.Name` on each must match an entry in `Manifest().Operations`.
- `HTTPHandler()` — mounted at the root of the plugin's HTTP server. The host
  reverse-proxies `/api/plugins/<name>/ui/api/*` here. The host doesn't know
  what these endpoints do; they are entirely the plugin's concern.

The entry point is `sdk.Serve(impl, opts...)`
([`plugin/sdk/serve.go`](../plugin/sdk/serve.go)). It validates the magic
cookie, binds an HTTP listener on `127.0.0.1:0`, starts the gRPC server, and
blocks until the host disconnects. Pass `sdk.WithStaticAssets(uiAssets)` to
embed a Vite-built UI alongside the plugin's API routes.

## The `Plugin` CRD

Every plugin ships a `Plugin.yaml` (Kubernetes CRD, `mission-control.flanksource.com/v1`):

```yaml
apiVersion: mission-control.flanksource.com/v1
kind: Plugin
metadata:
  name: golang
spec:
  source: golang             # binary name; supervisor execs this
  version: "0.1.0"           # declared binary version
  selector:
    types:                   # catalog item types this plugin attaches to
      - Kubernetes::Pod
      - Kubernetes::Deployment
  connections:               # connection-type allowlist (see GetConnection)
    kubernetes: {}
```

- `spec.source` — name of the plugin binary.
- `spec.selector.types` — which catalog item types invoke this plugin's tabs
  and operations.
- `spec.connections` — an **allowlist**. The host enforces this on every
  `HostClient.GetConnection` call: a plugin requesting a connection type it
  did not declare gets rejected at the host.

## gRPC: `PluginService` (host → plugin)

Defined in [`plugin/proto/plugin.proto`](../plugin/proto/plugin.proto). The
SDK implements all six on the plugin's behalf — authors don't write gRPC
handlers, they implement the `Plugin` interface above.

### `RegisterPlugin(RegisterRequest) → PluginManifest`

```proto
message RegisterRequest {
  uint32 host_protocol_version = 1;
  string host_version          = 2;
  uint32 host_broker_id        = 3;   // go-plugin reverse-channel broker id
  map<string,string> env       = 4;
}

message PluginManifest {
  string name              = 1;
  string version           = 2;
  string description       = 3;
  uint32 protocol_version  = 4;
  repeated string capabilities = 5;
  repeated TabSpec      tabs       = 6;
  repeated OperationDef operations = 7;
  uint32 ui_port           = 8;       // SDK fills this
}
```

Called once on startup. The SDK uses `host_broker_id` to dial the host's
reverse-channel for `HostService` calls. The `ui_port` field is set by the
SDK from the HTTP listener it bound; the host uses it to reverse-proxy UI
traffic.

### `Configure(ConfigureRequest) → ConfigureResponse`

```proto
message ConfigureRequest  { google.protobuf.Struct settings = 1; }
message ConfigureResponse { repeated string warnings = 1; }
```

Host pushes the merged CRD `spec.properties` plus host-side overrides. The
SDK decodes the `Struct` to `map[string]any` and calls your `Configure()`.
Return non-fatal validation issues as `warnings`; return an error to fail the
configuration.

### `ListOperations(Empty) → OperationList`

```proto
message OperationList { repeated OperationDef operations = 1; }
```

Lets the host refresh the operation list without re-registering. The SDK
fills this from `Plugin.Operations()`.

### `Invoke(InvokeRequest) → InvokeResponse`

```proto
message InvokeRequest {
  string operation      = 1;
  bytes  params_json    = 2;          // JSON body matching OperationDef.params_schema
  string config_item_id = 3;          // empty for global-scoped operations
  CallerContext caller  = 4;
  google.protobuf.Timestamp deadline = 5;
}

message InvokeResponse {
  bytes  result        = 1;
  string mime          = 2;           // typically application/clicky+json
  string error_message = 3;
  string error_code    = 4;
  repeated LogEntry logs = 5;
}

message CallerContext {
  string user_id              = 1;
  string user_email           = 2;
  repeated string permissions = 3;
  string trace_id             = 4;
  string request_id           = 5;
}
```

The SDK looks up the matching `Operation`, builds an `InvokeCtx` (with
`HostClient`, `Caller`, `ConfigItemID`, raw `ParamsJSON`), and calls the
handler. Handlers should respect `deadline` via `context.WithDeadline`.

### `Health(Empty) → HealthStatus`

```proto
message HealthStatus { bool ok = 1; string message = 2; }
```

Periodic liveness probe.

### `Shutdown(Empty) → Empty`

Graceful shutdown. The SDK closes the HTTP server and exits; the supervisor
treats a graceful exit as authoritative and does not restart.

## gRPC: `HostService` (plugin → host, reverse channel)

Plugin authors do **not** call this gRPC service directly — they go through
[`HostClient`](../plugin/sdk/host_client.go) on the `InvokeCtx`. The SDK
holds the reverse-channel connection that the host opened during
`RegisterPlugin`.

### `GetConfigItem(GetConfigItemRequest) → ConfigItem`

```proto
message GetConfigItemRequest { string id = 1; }

message ConfigItem {
  string id        = 1;
  string name      = 2;
  string type      = 3;
  string namespace = 4;
  string agent_id  = 5;
  google.protobuf.Struct properties = 6;
  google.protobuf.Struct config     = 7;
  map<string,string> labels = 8;
  map<string,string> tags   = 9;
  string health = 10;
  string status = 11;
}
```

The host validates the calling user's read permission before returning.

### `ListConfigs(ListConfigsRequest) → ConfigItemList`

```proto
message ListConfigsRequest {
  string selector_json = 1;   // JSON-encoded duty/types.ResourceSelector
  int32  limit         = 2;
  string cursor        = 3;
}
message ConfigItemList { repeated ConfigItem items = 1; string next_cursor = 2; }
```

Pass `selector_json` as opaque JSON — `json.Marshal` a map; you do not need
to import `duty` in the plugin just to build a selector.

### `GetConnection(GetConnectionRequest) → ResolvedConnection`

```proto
message GetConnectionRequest {
  string type           = 1;   // "aws" | "kubernetes" | "gcp" | "azure"
  string config_item_id = 2;   // optional: derive creds from this catalog item
}

message ResolvedConnection {
  string type        = 1;
  string url         = 2;
  string username    = 3;
  string password    = 4;
  string certificate = 5;
  string token       = 6;
  google.protobuf.Struct properties = 7;
  google.protobuf.Timestamp expires_at = 8;
}
```

Resolves credentials through the same `SetupConnection()` pipeline that
playbook exec actions use. **Enforced against `Plugin.spec.connections`** —
requesting an undeclared type fails. Resolved connections are cached
host-side for ~5 minutes.

### `Log(LogEntry) → Empty`

```proto
message LogEntry {
  string level   = 1;          // debug | info | warn | error
  string message = 2;
  map<string,string> fields = 3;
  google.protobuf.Timestamp ts = 4;
}
```

### `WriteArtifact(Artifact) → ArtifactRef`  /  `ReadArtifact(ArtifactRef) → Artifact`

```proto
message Artifact {
  string name         = 1;
  string content_type = 2;
  bytes  data         = 3;
  map<string,string> metadata = 4;
}
message ArtifactRef { string id = 1; string url = 2; }
```

Persist large outputs (profile dumps, logs, reports) via the host's artifact
store and return the ref to the caller; resolve the ref later from another
operation or the UI.

## Operations and clicky

```go
type Operation struct {
    Def     *pluginpb.OperationDef
    Handler func(ctx context.Context, req InvokeCtx) (any, error)
}

type OperationDef struct {
    Name                string
    Description         string
    ParamsSchema        *structpb.Struct  // JSON Schema describing params_json
    ResultMime          string            // ClickyResultMimeType for clicky output
    Scope               string            // "config" or "global"
    Destructive         bool              // host requires extra confirmation
    RequiredPermissions []string
}
```

Handlers return `(any, error)`. The SDK marshals the value via
[`ClickyResult`](../plugin/sdk/clicky.go) (`encoding/json`) and ships it back
as `application/clicky+json`. Return a domain struct that implements
[clicky's][clicky] `Pretty()` interface — rendering happens on the receiving
side (terminal width, color capabilities, browser), so the wire format stays
neutral. For pre-encoded payloads, return `json.RawMessage`.

- **clicky** (RPC framing + rendering framework): <https://github.com/flanksource/clicky>
- Pinned at `v1.21.8` in [`go.mod`](../go.mod).

Use `Scope: "config"` if the operation requires a `config_item_id`;
`"global"` for operations the user invokes from a top-level menu.

## UI

Plugins ship UI as embedded static assets:

```go
//go:embed all:ui
var uiAssets embed.FS

func main() {
    sub, _ := fs.Sub(uiAssets, "ui")
    sdk.Serve(newPlugin(), sdk.WithStaticAssets(sub))
}
```

- The plugin's HTTP server serves your static bundle at the root and your
  `HTTPHandler()` at whatever paths you claim. The SDK does request buffering
  so a `404` from your handler falls through to the static server (so SPA
  routes still work). Streaming responses are committed as soon as you call
  `Flush` or `Hijack` and never fall through.
- The host reverse-proxies `/api/plugins/<name>/ui/*` to the plugin's HTTP
  port (advertised in the manifest). See
  [`plugin/controller/controller.go`](../plugin/controller/controller.go).
- **Cache-busting**: include the UI bundle's sha in the manifest version via
  `sdk.FormatVersion(Version, BuildDate, uiChecksum)`. Rebuilding the UI
  changes the version, which busts the iframe cache. See
  [`plugins/golang/ui_checksum.go`](golang/ui_checksum.go) for the pattern.
- **Widget types** (used by `inspektor-gadget`): an operation can declare
  itself as `trace`, `top`, `snapshot`, `profile`, `report`, or `table` and
  the frontend picks the rendering strategy accordingly.

### UI best practices

- **Talk to your own `HTTPHandler()`** for plugin data; do not try to call
  the host's gRPC API from the browser. The browser is sandboxed inside the
  iframe, and `HostClient` is a Go-only contract.
- **Use clicky-ui semantic tokens** (`text-primary`, `bg-surface`, etc.) and
  Tailwind utilities. No CSS-in-JS, no inline `style={...}`, no
  `CSSProperties` constants.
- **Always emit source maps** in your Vite config — they ship in the embedded
  bundle. There is no `PLUGIN_UI_RELEASE` toggle.
- **Use Tailwind text-size utilities** (`text-xs`, `text-sm`, `text-base`,
  `text-lg`) — never hard-coded `pt`/`px` sizes.
- **Keep the UI thin.** The right split is: data + transformation in Go
  (operation handlers), presentation in the UI. If you find yourself
  re-implementing a query in TypeScript, push it back into a handler.

## Logging

Plugins have two output channels, both legitimate:

- **`HostClient.Log(ctx, level, message, fields)`** — structured, audit-grade
  events that flow through the host's logger and end up in operator-visible
  logs. Use this for user actions, connection resolution, and errors anyone
  might want to query later.
- **`stderr`** — `fmt.Fprintf(os.Stderr, ...)`, `log.Print`, `slog.Debug`.
  `go-plugin` captures the plugin's stderr and routes it through the host
  logger as plugin-tagged debug output. Use this for development noise that
  has no value in production logs.

Rule of thumb: if an operator might want to grep for it tomorrow, use
`Host.Log`; otherwise use stderr.

## Errors

- Return errors from operation handlers; the SDK puts them in
  `InvokeResponse.error_message` / `error_code`.
- Don't swallow errors. Don't fall back to a default value to "keep things
  working". A loud failure you can fix beats a quiet one you can't see.
- For `GetConnection`, surface the host's allowlist error as-is — don't
  rewrap it as "internal error". The user can fix the CRD; an opaque message
  hides the cause.
- Workarounds for upstream bugs require a `// WORKAROUND(reason):` comment
  and explicit user sign-off. (See repo `CW-*` rules.)

## Lifecycle and supervision (reference)

| Detail | Where |
|---|---|
| Magic cookie key/value | [`plugin/handshake.go`](../plugin/handshake.go): `MISSION_CONTROL_PLUGIN=mission-control-plugin/v1` |
| Protocol version | [`plugin/handshake.go`](../plugin/handshake.go): `ProtocolVersion = 1` (bump on breaking proto changes) |
| Plugin name in PluginMap | [`plugin/handshake.go`](../plugin/handshake.go): `mission-control` (one plugin per binary) |
| Register deadline | [`plugin/supervisor/supervisor.go`](../plugin/supervisor/supervisor.go): 30s |
| Restart budget | [`plugin/supervisor/supervisor.go`](../plugin/supervisor/supervisor.go): 10/hour |
| Manifest cache | [`plugin/manifestcache/`](../plugin/manifestcache/) — used by CLI for `mission-control <plugin> --help` |

## Existing plugins

| Plugin | Purpose |
|---|---|
| [`golang/`](golang/) | Go runtime introspection — gops, pprof, profile viewer, multi-port discovery |
| [`kubernetes-logs/`](kubernetes-logs/) | Pod log streaming over chunked HTTP |
| [`inspektor-gadget/`](inspektor-gadget/) | eBPF gadget runs with widget-typed event streams |
| [`postgres/`](postgres/) | Postgres introspection — sessions, locks, schema, console |
| [`sql-server/`](sql-server/) | SQL Server introspection |
| [`arthas/`](arthas/) | JVM diagnostics via Arthas |

[go-plugin]: https://github.com/hashicorp/go-plugin
[clicky]: https://github.com/flanksource/clicky
