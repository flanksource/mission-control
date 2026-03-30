# Repository Guidelines

- Don't run `go build`. Use `make dev` instead.
- Core database models come from `github.com/flanksource/duty/models`.
- The local `api/` directory contains API-specific request/response types and CRD specs (`api/v1/`).

## Key Libraries & Imports

| Import                                | Purpose                                      |
| ------------------------------------- | -------------------------------------------- |
| `github.com/flanksource/duty/context` | Primary context — DB, logger, properties     |
| `github.com/flanksource/duty/models`  | Database models (configs, connections, etc.) |
| `github.com/flanksource/duty/api`     | Error codes (`EINVALID`, `ENOTFOUND`, etc.)  |
| `github.com/labstack/echo/v4`         | HTTP framework                               |
| `github.com/spf13/cobra`              | CLI framework                                |
| `github.com/onsi/ginkgo/v2`           | Test framework                               |
| `github.com/onsi/gomega`              | Test matchers                                |
| `github.com/flanksource/duty/job`     | Background job scheduling                    |
| `github.com/flanksource/postq`        | PostgreSQL event queue                       |
| `github.com/flanksource/kopper`       | Kubernetes CRD reconcilers                   |

## The Context Object

There are three context types in this codebase. Getting them confused is the most common mistake.

### `duty/context.Context` (primary)

Used everywhere. Provides:

- `ctx.DB()` — returns `*gorm.DB`
- `ctx.Logger` — structured logger (e.g. `ctx.Logger.Infof("msg")`)
- `ctx.Properties()` — runtime configuration
- `ctx.Oops()` — error builder (see [Errors](#errors))

### `echo.Context` (HTTP handlers only)

The argument to Echo handler functions. Extract the duty context:

```go
ctx := c.Request().Context().(context.Context)
```

### Standard `context.Context`

When a file needs both `duty/context` and the standard library `context`, alias the standard one:

```go
import (
    gocontext "context"
    "github.com/flanksource/duty/context"
)
```

## Coding Guidelines

- Prefer `any` over `interface{}`
- Use `duty/context.Context` for all function signatures unless there's a specific reason not to.

## Route Registration & HTTP Handlers

### Registering routes

Routes are registered via `init()` + `echoSrv.RegisterRoutes()`:

```go
import echoSrv "github.com/flanksource/incident-commander/echo"

func init() {
    echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
    group := e.Group("/myresource")
    group.GET("/:id", GetHandler, rbac.Authorization(policy.ObjectCatalog, policy.ActionRead))
    group.POST("", CreateHandler, rbac.Authorization(policy.ObjectCatalog, policy.ActionWrite))
}
```

### Writing handlers

```go
func GetHandler(c echo.Context) error {
    ctx := c.Request().Context().(context.Context)

    item, err := query.FindItem(ctx, c.Param("id"))
    if err != nil {
        return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
    }

    return c.JSON(http.StatusOK, item)
}
```

**Rules:**

- Always return errors via `dutyAPI.WriteError(c, err)`.
- Never return errors directly (`return ctx.Oops().Wrap(err)`) or manually construct HTTP error responses.
- For validation errors with no underlying error: `dutyAPI.Errorf(dutyAPI.EINVALID, "message")`
- For wrapping internal errors: `ctx.Oops().Wrap(err)` or `ctx.Oops().Wrapf(err, "context")`
- For permission errors: `ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("message")`

**Error code → HTTP status mappings:**

| Code                    | HTTP Status               |
| ----------------------- | ------------------------- |
| `dutyAPI.EINVALID`      | 400 Bad Request           |
| `dutyAPI.EUNAUTHORIZED` | 401 Unauthorized          |
| `dutyAPI.EFORBIDDEN`    | 403 Forbidden             |
| `dutyAPI.ENOTFOUND`     | 404 Not Found             |
| `dutyAPI.ECONFLICT`     | 409 Conflict              |
| `dutyAPI.EINTERNAL`     | 500 Internal Server Error |

### RBAC

Authorization is enforced per-route using `rbac.Authorization(object, action)` as Echo middleware:

```go
group.POST("/test/:id", TestConnection, rbac.Authorization(policy.ObjectConnection, policy.ActionUpdate))
```

For proxied route groups, use `rbac.DbMiddleware()`:

```go
Forward(ctx, e, "/db", url, &ForwardOptions{
    Middlewares: []echo.MiddlewareFunc{rbac.DbMiddleware()},
})
```

## CLI Commands

Commands use Cobra and are added to `Root` via `init()`:

```go
var MyCmd = &cobra.Command{
    Use:   "mycmd",
    Short: "Does something",
    Run: func(cmd *cobra.Command, args []string) {
        // ...
    },
}

func init() {
    Root.AddCommand(MyCmd)
}
```

See `cmd/root.go` for the root command definition and flag binding.

## Database & Models

- `ctx.DB()` returns a `*gorm.DB` instance.
- Core models live in `github.com/flanksource/duty/models`.
- Migrations are handled by `github.com/flanksource/duty` using Atlas-go.
- Use `duty.Now() time.Time` instead of `time.Now() gorm.Expr` for database timestamps and soft deletes.
  This only applies when the parameters are map<> because they are different types.

```go
// Query
var conn models.Connection
if err := ctx.DB().Where("id = ?", id).First(&conn).Error; err != nil {
    return ctx.Oops().Wrap(err)
}

// Create
if err := ctx.DB().Create(&record).Error; err != nil {
    return ctx.Oops().Wrap(err)
}

// Soft delete
ctx.DB().Model(&models.Connection{}).Where("id = ?", id).Update("deleted_at", duty.Now())
```

### To connect to local database

```sh
psql $DB_URL -c "SELECT VERSION()"
```

## Background Jobs

Jobs are defined using `duty/job` and scheduled via `jobs.FuncScheduler`:

```go
func Start(ctx context.Context) {
    if err := job.NewJob(ctx, "My Job", "@every 15m", myJobFunc).
        RunOnStart().AddToScheduler(jobs.FuncScheduler); err != nil {
        logger.Errorf("Failed to schedule My Job: %v", err)
    }
}
```

Or as a struct for more options:

```go
func MyJob(ctx context.Context) *job.Job {
    return &job.Job{
        Name:       "MyJob",
        Schedule:   "@every 10m",
        Context:    ctx,
        Singleton:  true,
        Retention:  job.RetentionFailed,
        JobHistory: true,
        RunNow:     true,
        Fn: func(ctx job.JobRuntime) error {
            return doWork(ctx.Context)
        },
    }
}
```

## Events

Events use a PostgreSQL-backed queue. Register handlers in `init()` via `events.Register()`:

```go
func init() {
    events.Register(RegisterEvents)
}

func RegisterEvents(ctx context.Context) {
    // Synchronous — blocks the event loop
    events.RegisterSyncHandler(handleStatusChange, api.EventStatusGroup...)

    // Asynchronous — processed in batches with configurable consumers
    events.RegisterAsyncHandler(sendNotifications, 1, 5, api.EventNotificationSend)
    // args: handler func, batchSize, numConsumers, event names...
}
```

## Errors

- Use `ctx.Oops()` (method call, not field access) to build errors.
- Use `.Code()` with error codes from `github.com/flanksource/duty/api`.
- Use `.Wrapf()` / `.Wrap()` to wrap underlying errors with context.
- Use `oops.With()` to add structured error context.

```go
return ctx.Oops().Wrapf(err, "playbook %s not found", playbookID)
return ctx.Oops().Code(dutyAPI.EINVALID).Errorf("invalid input")
```

## CRD Lifecycle

Full checklist for adding or modifying a CRD:

1. **Define the spec** in `api/v1/` (e.g. `api/v1/myresource_types.go`).
2. **Write persist/delete functions** in `db/`:

   ```go
   func PersistMyResourceFromCRD(ctx context.Context, obj *v1.MyResource) error {
       dbObj := MyResourceFromCRD(obj)
       return ctx.DB().Save(&dbObj).Error
   }

   func DeleteMyResource(ctx context.Context, id string) error {
       return ctx.DB().Model(&models.MyResource{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
   }

   func DeleteStaleMyResource(ctx context.Context, newer *v1.MyResource) error {
       return ctx.DB().Model(&models.MyResource{}).
           Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
           Where("deleted_at IS NULL").
           Update("deleted_at", duty.Now()).Error
   }
   ```

3. **Register the reconciler** in `cmd/server.go` → `launchKopper()`:
   ```go
   kopper.SetupReconciler(ctx, mgr,
       db.PersistMyResourceFromCRD,
       db.DeleteMyResource,
       db.DeleteStaleMyResource,
       "myresource.mission-control.flanksource.com",
   )
   ```
4. **Run `make manifests`** to regenerate CRDs in `config/crds/` and `config/schemas/`.
5. **Run `make generate`** if you changed struct fields (regenerates `zz_generated.deepcopy.go`).

**Never manually edit** files in `config/crds/`, `config/schemas/`, or `zz_generated.deepcopy.go`.

## Tests

- `make test` — runs all tests except e2e.
- `make e2e` — runs e2e tests in `tests/e2e/`.
- To run a specific test: `ginkgo -focus "TestName" -r`
- To run tests in a package: `ginkgo --label-filter='!ignore_local' ./path/to/pkg/`
- Always use ginkgo to run tests. Never run `go test` directly.
- Always use `github.com/onsi/gomega` for assertions.

### Writing new tests

- All tests must use Ginkgo v2 (`github.com/onsi/ginkgo/v2`) with gomega matchers.
- Do NOT write native `func TestXxx(t *testing.T)` tests. Use `ginkgo.Describe`/`ginkgo.It` blocks instead.
  - Exception: the single `func TestMyPkg(t *testing.T)` in `suite_test.go` is the Ginkgo bootstrap — this is required.
- Use dot-import for gomega (`. "github.com/onsi/gomega"`) so you can write `Expect(...)` directly.
- Use qualified import for ginkgo (`ginkgo "github.com/onsi/ginkgo/v2"`) to avoid name collisions (e.g. `Context` type).
- For table-driven tests, use a `for` loop with `ginkgo.It` per case:

```go
for _, tt := range tests {
    ginkgo.It(tt.name, func() {
        Expect(myFunc(tt.input)).To(Equal(tt.expected))
    })
}
```

- Do NOT copy loop variables (`tt := tt`) — Go 1.22+ captures them correctly.

### Suite structure

Every package with tests must have a `suite_test.go` with a Ginkgo bootstrap:

```go
package mypkg

import (
    "testing"
    ginkgo "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
)

func TestMyPkg(t *testing.T) {
    RegisterFailHandler(ginkgo.Fail)
    ginkgo.RunSpecs(t, "MyPkg")
}
```

- Only add `ginkgo.BeforeSuite`/`ginkgo.AfterSuite` if tests need database or external service setup.
- Use `ginkgo.Label("ignore_local")` for tests requiring external services.
- Use `ginkgo.Label("slow")` for tests taking more than 10 seconds.
- Use `ginkgo.Ordered` only when test steps must run sequentially.

## Comments Guidelines

- Only add comments if really really necessary. Do not add comments that simply explain the code.
  - Exception: comments about functions are considered good practice in Go even if they are self-explanatory.
