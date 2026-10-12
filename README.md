# sqlctxize

sqlctxize rewrites `database/sql` and [sqlx](https://github.com/jmoiron/sqlx) calls into their `context.Context` variants and threads a `ctx` to them, so that DB spans (e.g. otelsql) nest under the HTTP request span.

```go
// before
func getLivestreamsHandler(c echo.Context) error {
	tx, err := dbConn.Beginx()
	...
	l, err := fillLivestreamResponse(tx, model)
}

func fillLivestreamResponse(tx *sqlx.Tx, l LivestreamModel) (Livestream, error) {
	if err := tx.Get(&owner, "SELECT * FROM users WHERE id = ?", l.UserID); err != nil {
```

```go
// after
func getLivestreamsHandler(c echo.Context) error {
	ctx := c.Request().Context()
	tx, err := dbConn.BeginTxx(ctx, nil)
	...
	l, err := fillLivestreamResponse(ctx, tx, model)
}

func fillLivestreamResponse(ctx context.Context, tx *sqlx.Tx, l LivestreamModel) (Livestream, error) {
	if err := tx.GetContext(ctx, &owner, "SELECT * FROM users WHERE id = ?", l.UserID); err != nil {
```

## Install

```bash
go install github.com/p1ass/sqlctxize/cmd/sqlctxize@latest
```

sqlctxize requires Go 1.26 or later to build. It can only type-check modules whose `go` directive is not newer than the Go it was built with. With the default `GOTOOLCHAIN=auto`, `go install` from an older Go switches to the latest Go 1.26.x. For a module that declares a newer Go, reinstall with that toolchain, e.g. `GOTOOLCHAIN=go1.27.0 go install github.com/p1ass/sqlctxize/cmd/sqlctxize@latest`. sqlctxize prints this command when it is needed.

## Execute

```bash
# print the rewritten files
sqlctxize -dir ./webapp/go

# overwrite the files
sqlctxize -w -dir ./webapp/go -exclude isuutil
```

| Flag | Description |
| --- | --- |
| `-dir` | Directory of the module to rewrite. All packages under it (`./...`) are processed. Default: `.` |
| `-w` | Write the result to the source files instead of stdout. |
| `-exclude` | Comma-separated package directories relative to `-dir` to leave untouched. A directory also excludes its subdirectories, and glob patterns (`path.Match`) are allowed. |

The packages must compile before running. Running it again changes nothing, and the output is gofmt-ed.

## What it rewrites

### DB calls

Methods of `*sql.DB`, `*sql.Tx`, `*sql.Conn`, `*sql.Stmt`, `*sqlx.DB`, `*sqlx.Tx`, `*sqlx.Conn`, `*sqlx.Stmt`, `*sqlx.NamedStmt`, types embedding them, and interfaces that declare both variants.

| Before | After |
| --- | --- |
| `Query`, `QueryRow`, `Exec`, `Prepare`, `Ping` | `QueryContext(ctx, ...)` etc. |
| `Get`, `Select`, `Queryx`, `QueryRowx`, `MustExec`, `NamedExec`, `NamedQuery`, `Preparex`, `PrepareNamed`, `Stmt`, `Stmtx`, `NamedStmt` | `GetContext(ctx, ...)` etc. |
| `Begin`, `Beginx`, `MustBegin` | `BeginTx(ctx, nil)`, `BeginTxx(ctx, nil)`, `MustBeginTx(ctx, nil)` |
| `sqlx.Connect`, `sqlx.Get`, `sqlx.Select`, `sqlx.Preparex`, `sqlx.MustExec`, `sqlx.LoadFile` | `sqlx.ConnectContext(ctx, ...)` etc. |

A call is rewritten only when the receiver has the `Context` variant and it accepts the same arguments. Methods without one, such as `Commit`, `Rollback`, `Rebind`, `sqlx.In` and `(*sqlx.Tx).NamedQuery`, are left as they are.

### Where `ctx` comes from

For each rewritten call, sqlctxize uses the first of these that applies, from the innermost function outwards.

1. A `context.Context` variable already in scope, such as a `ctx` parameter or `ctx` from `errgroup.WithContext`.
2. In a function with an `echo.Context` parameter `c`, it adds `ctx := c.Request().Context()` at the top.
3. In a function with an `*http.Request` parameter `r` (net/http, chi), it adds `ctx := r.Context()` at the top.
4. Otherwise it adds a `ctx context.Context` parameter to the function and passes `ctx` at every call site, which repeats these steps for the callers.
5. Where a parameter cannot be added, it adds `ctx := context.Background()` at the top. That is `main`, `init`, functions used as values (e.g. `run(cleanup)`), and methods that implement an interface (e.g. `String()`). Package-level variable initializers use `context.Background()` directly.

Func literals such as `eg.Go(func() error { ... })` and `once.Do(func() { ... })` use the `ctx` of the enclosing function. Functions without DB calls, directly or through their callees, are not changed. The `context` import is added only to files that need it.

## Known limitations

- Only packages under `-dir` are analyzed. Callers outside it, including `_test.go` files and packages excluded with `-exclude`, are not updated when a function gains a `ctx` parameter.
- Calls through function values and interface methods are not followed, so their implementations fall back to `context.Background()`.
- Web frameworks other than echo and net/http (e.g. gin) are not detected as `ctx` sources.
- `go/types` cannot type-check a module whose `go` directive is newer than the Go that built sqlctxize. See [Install](#install).
