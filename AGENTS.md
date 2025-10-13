# Repository Guidelines

- Domain models and REST API models are in @api/ directory.
- Don't run `go build`. Use `make dev` instead.

### Coding guidelines

- we use context from github.com/flanksource/duty/context.Context for most things. That context is how we access
  - db (via ctx.DB())
  - properties (via ctx.Properties())
  - logger (via ctx.Logger. Example: ctx.Logger.Infof("Hello, world!"))
- If a file needs to use both context and duty's context, alias context to "gocontext".

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
