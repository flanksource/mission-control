# Repository Guidelines

- Domain models and REST API models are in @api/ directory.
- Don't run `go build`. Use `make dev` instead.

### Coding guidelines

- we use context from github.com/flanksource/duty/context.Context for most things. That context is how we access
  - db (via ctx.DB())
  - properties (via ctx.Properties())
  - logger (via ctx.Logger. Example: ctx.Logger.Infof("Hello, world!"))
- If a file needs to use both context and duty's context, alias context to "gocontext".
- Prefer `any` over `interface{}`

### CRD

- CRD definitions are in the api/v1 directory.
- Whenever the CRD definition is updated, run `make manifests`.
- never manually update files in @config/crds and @config/schemas. Always use `make manifests` to update the CRDs.
- never update `zz_generated.deepcopy.go` files. use `make generate` instead.

### Tests

- To run the entire test suite, run `make test`.
- To run a specific test. use `ginkgo -focus "TestName" -r`
- To run tests in a package, use ginkgo with `--label-filter='!ignore_local'` flag.
- Always use ginkgo to run tests. Never run `go test` directly.
- Always use `github.com/onsi/gomega` package for assertions.
- When using gomega with native go tests use this approach

```go
g := gomega.NewWithT(t)
g.Expect(true).To(gomega.Equal(1 == 1))
```

### Errors

- Use ctx.Oops() to craft new errors.
- Use the error codes from `github.com/flanksource/duty/api` as tags in Oops error.
  Example: `ctx.Oops.Tags(api.EINVALID).Wrapf(err, "playbook %s not found", playbook)`
- Use `oops.With()` to add error context using variadic arguments.

### HTTP Handlers (Echo)

HTTP handlers must use `dutyAPI.WriteError(c, err)` to return errors. 
This ensures proper HTTP status code mapping based on error codes and fixed API schema.

**Error codes and their HTTP status mappings:**

- `dutyAPI.EINVALID` → 400 Bad Request
- `dutyAPI.EUNAUTHORIZED` → 401 Unauthorized
- `dutyAPI.EFORBIDDEN` → 403 Forbidden
- `dutyAPI.ENOTFOUND` → 404 Not Found
- `dutyAPI.ECONFLICT` → 409 Conflict
- `dutyAPI.EINTERNAL` → 500 Internal Server Error

**Creating errors in handlers:**

- For validation errors with no underlying error: `dutyAPI.Errorf(dutyAPI.EINVALID, "message")`
- For wrapping database/internal errors: `ctx.Oops().Wrap(err)` or `ctx.Oops().Wrapf(err, "context")`
- For permission errors: `ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("message")`

**Examples:**

```go
func HandleExample(c echo.Context) error {
    ctx := c.Request().Context().(context.Context)

    var req Request
    if err := c.Bind(&req); err != nil {
        return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request: %v", err))
    }

    item, err := query.FindItem(ctx, req.ID)
    if err != nil {
        return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
    } else if item == nil {
        return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "item(id=%s) not found", req.ID))
    }

    if !hasPermission(ctx, item) {
        return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("access denied"))
    }

    return c.JSON(http.StatusOK, item)
}
```

**What NOT to do:**

- ❌ Don't return errors directly: `return ctx.Oops().Wrap(err)`
- ❌ Don't manually construct HTTP responses: `return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{...})`
- ✅ Always use: `return dutyAPI.WriteError(c, err)`

### Database

- Use `duty.Now()` instead of `time.Now()` for database timestamps and soft deletes.
- The migrations are handled by an external package `github.com/flanksource/duty` using Atlas-go.

### Comments guidelines

- Only add comments if really really necessary. Do not add comments that simply explain the code.
  - Exception: comments about functions are considered good practice in Go even if they are self-explanatory.

### To Connect to local database

Run

```sh
psql $DB_URL -c "SELECT VERSION()"
```
