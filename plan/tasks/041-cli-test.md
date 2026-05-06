# Task 041: CLI Test Command

**Phase:** 4 — CLI & DX  
**Depends on:** 035 (CLI Framework), 042 (Testing Harness)  
**Status:** not started

---

## Goal

Implement `gofastr test` — the test runner command that integrates with GoFastr's testing harness to run tests with framework context, test database management, coverage reporting, and watch mode. This provides a superior testing experience over bare `go test` by understanding the GoFastr project structure.

---

## Context

From the draft:

> ```go
> func TestListPosts(t *testing.T) {
>     res := gofastr.Test(t, app).Get("/posts")
>     res.AssertStatus(200)
>     res.AssertJSON(t, []Post{{Title: "Hello"}})
> }
> ```
>
> In-memory, no real HTTP server. Fast.

From the proposal:

> `gofastr test` → run tests with framework helpers

The testing harness (task 042) provides the `gofastr.Test()` API for in-memory HTTP testing. This CLI command wraps `go test` with project awareness, test database setup, coverage, and watch mode.

---

## Requirements

### 1. Command Definition

```
gofastr test [path...] [flags]
```

- `path...` is optional. Default: `./...` (all packages in project).
- Supports the same path patterns as `go test`: `./...`, `./pkg/...`, `./handlers/...`.

### 2. Flags

| Flag | Type | Default | Purpose |
|------|------|---------|---------|
| `--watch` / `-w` | bool | false | Watch mode: rerun tests on file changes. |
| `--verbose` / `-v` | bool | false | Verbose output (pass `-v` to `go test`). |
| `--cover` | bool | false | Enable coverage reporting. |
| `--cover-html` | string | `""` | Generate HTML coverage report to file. |
| `--cover-min` | float | `0` | Minimum coverage percentage. Fails if below. |
| `--race` | bool | false | Enable race detector. |
| `--bench` | string | `""` | Run benchmarks matching pattern. Passes `-bench` to `go test`. |
| `--benchmem` | bool | false | Show memory allocation in benchmarks. |
| `--run` | string | `""` | Run only tests matching pattern. Passes `-run` to `go test`. |
| `--short` | bool | false | Skip long-running tests. Passes `-short` to `go test`. |
| `--timeout` | duration | `5m` | Test timeout. |
| `--db` | string | from config | Test database driver. Default: `sqlite` (in-memory). |
| `--db-url` | string | auto | Test database URL. Default: `:memory:` for SQLite. |
| `--json` | bool | false | Machine-readable output (inherited from root). |
| `--failfast` | bool | false | Stop on first test failure. |

### 3. Test Pipeline

```
gofastr test
  │
  ├── Step 1: Project Discovery
  │   ├── Load config
  │   ├── Find all *_test.go files
  │   └── Determine test packages
  │
  ├── Step 2: Test Database Setup
  │   ├── If db=sqlite: use :memory: (no setup needed)
  │   ├── If db=postgres: create test database (myapp_test)
  │   ├── Run pending migrations on test database
  │   └── Seed test fixtures (if fixtures/ directory exists)
  │
  ├── Step 3: Generate (if needed)
  │   └── Run gofastr generate if .gofastr/ is stale
  │
  ├── Step 4: Run Tests
  │   ├── Build go test command:
  │   │   go test -v -race -cover -timeout 5m ./...
  │   ├── Set environment variables:
  │   │   GOFASTR_TEST=1
  │   │   GOFASTR_DB_URL=<test-db-url>
  │   │   GOFASTR_CONFIG=<config-path>
  │   ├── Capture output
  │   └── Parse results
  │
  ├── Step 5: Report Results
  │   ├── Human: formatted test output with summary
  │   └── JSON: structured results for CI
  │
  └── Step 6: Cleanup
      ├── Drop test database (if created)
      └── Remove temp files
```

### 4. Test Database Management

#### SQLite (default)

- Use `:memory:` for each test package. No setup or cleanup needed.
- Each test gets its own isolated in-memory database via the test harness.

#### PostgreSQL

- Create a test database: `<dbname>_test` (e.g., `myapp_test`).
- If it exists, drop and recreate (or truncate all tables).
- Run migrations on the test database.
- After tests: optionally drop the test database (`--no-cleanup` to keep it).

#### Test fixtures

If a `fixtures/` directory exists:

```
fixtures/
├── users.json       → seed data for users table
├── posts.json       → seed data for posts table
└── ...
```

Load fixtures into the test database after migrations. Format:

```json
[
  {"id": "1", "title": "First Post", "body": "Hello World", "published": true},
  {"id": "2", "title": "Second Post", "body": "Goodbye World", "published": false}
]
```

### 5. Output Formatting

#### Human mode (default)

```
Running tests...

  ✓ handlers/post_test.go  TestListPosts           (0.02s)
  ✓ handlers/post_test.go  TestGetPost             (0.01s)
  ✓ handlers/post_test.go  TestCreatePost          (0.03s)
  ✗ handlers/post_test.go  TestUpdatePost          (0.01s)
    Error: expected status 200, got 404
    at post_test.go:45
  ✓ handlers/user_test.go  TestListUsers           (0.01s)

  Results: 4 passed, 1 failed, 0 skipped (0.08s)
  Coverage: 67.3%

  Failed tests:
    TestUpdatePost — expected status 200, got 404
      Fix: Check that the post exists before updating

  Run with --verbose for full output.
```

#### JSON mode (`--json`)

```json
{
  "status": "error",
  "data": {
    "total": 5,
    "passed": 4,
    "failed": 1,
    "skipped": 0,
    "duration_ms": 80,
    "coverage_pct": 67.3,
    "packages": [
      {
        "name": "handlers",
        "tests": [
          {"name": "TestListPosts", "status": "pass", "duration_ms": 20},
          {"name": "TestGetPost", "status": "pass", "duration_ms": 10},
          {"name": "TestCreatePost", "status": "pass", "duration_ms": 30},
          {
            "name": "TestUpdatePost",
            "status": "fail",
            "duration_ms": 10,
            "error": "expected status 200, got 404",
            "file": "post_test.go",
            "line": 45
          }
        ]
      }
    ]
  }
}
```

### 6. Watch Mode (`--watch`)

Same file watching as `gofastr dev` (task 039):

- Watch `.go` files, entity files, templates.
- Ignore `.gofastr/`, `.git/`, `vendor/`.
- Debounce: 500ms (longer than dev since tests take longer).
- On file change:
  1. Print "Tests rerunning..."
  2. Rerun the test pipeline.
  3. Print new results.
  4. Keep watching.

#### Watch mode output

```
Running tests (watch mode)...
Press Ctrl+C to stop.

  [initial run]
  ✓ 4 passed, 0 failed (0.08s)

  [change: handlers/post.go]
  Rerunning tests...
  ✓ 3 passed, 1 failed (0.09s)
  ✗ TestUpdatePost — expected status 200, got 404

  [change: handlers/post.go]
  Rerunning tests...
  ✓ 4 passed, 0 failed (0.07s)
```

### 7. Coverage Reporting

#### `--cover`

Runs tests with `-coverprofile=coverage.out`.

Print summary per package:

```
Coverage:
  handlers/    78.5%
  models/      92.1%
  middleware/  45.0%
  ───────────────────
  Total:       67.3%
```

#### `--cover-html <path>`

Runs `go tool cover -html=coverage.out -o <path>` after tests.

```
✓ HTML coverage report generated: coverage.html
  Open in browser: file:///path/to/coverage.html
```

#### `--cover-min 80`

Fail the test run if total coverage is below 80%.

```
✗ Coverage 67.3% is below minimum 80%
  Add more tests or adjust --cover-min.
```

### 8. Benchmark Support

#### `--bench .`

Runs all benchmarks:

```
BenchmarkListPosts-8       50000    28345 ns/op    4096 B/op    52 allocs/op
BenchmarkCreatePost-8      20000    58123 ns/op    8192 B/op    98 allocs/op
```

#### `--bench ListPosts`

Runs benchmarks matching the pattern.

#### `--benchmem`

Includes memory allocation stats.

### 9. Environment Variables

The test command sets these environment variables for the test process:

| Variable | Value | Purpose |
|----------|-------|---------|
| `GOFASTR_TEST` | `1` | Indicates test mode. Framework can adjust behavior. |
| `GOFASTR_DB_URL` | test DB URL | Test database connection. |
| `GOFASTR_DB_DRIVER` | test DB driver | Test database driver. |
| `GOFASTR_CONFIG` | config path | Config file path. |
| `GOFASTR_ENV` | `test` | Environment name. |

The testing harness (task 042) reads these to configure itself.

### 10. Integration with Testing Harness

The test command doesn't replace `go test` — it enhances it. The testing harness (task 042) provides the Go API:

```go
// In user's test file:
func TestListPosts(t *testing.T) {
    res := gofastr.Test(t, app).Get("/posts")
    res.AssertStatus(200)
}
```

The CLI command:
- Sets up the environment (test DB, env vars).
- Runs `go test` with appropriate flags.
- Parses and formats the output.
- Provides watch mode and coverage.

### 11. CI Integration

`--json` mode is designed for CI/CD pipelines:

```yaml
# GitHub Actions example
- name: Run tests
  run: gofastr test --json --cover --race --cover-min 80 > test-results.json
  
- name: Upload coverage
  if: always()
  run: gofastr test --cover-html coverage.html
```

Exit codes:
- `0` — all tests pass
- `1` — one or more tests failed
- `2` — test infrastructure error (no config, database connection failed, etc.)

---

## Error Handling

| Error | Message | Suggestion |
|-------|---------|------------|
| No test files found | `No *_test.go files found in the project.` | `Create test files: handlers/post_test.go, models/user_test.go` |
| Test database setup failed | `Failed to create test database: <error>` | `Check your database config or use --db sqlite for in-memory testing.` |
| Migration failed on test DB | `Migrations failed on test database: <error>` | `Run 'gofastr migrate status' to check migration state. Use --db sqlite to avoid migration issues.` |
| Coverage below minimum | `Coverage 45.2% is below minimum 80%.` | `Add more tests. Run 'gofastr test --cover-html coverage.html' to see uncovered code.` |
| Test timeout | `Tests timed out after 5m.` | `Use --timeout to increase the timeout, or --short to skip slow tests.` |

---

## Acceptance Criteria

- [ ] `gofastr test` discovers and runs all `*_test.go` files in the project
- [ ] Test results are formatted with pass/fail status and timing
- [ ] Failed tests show error message, file, and line number
- [ ] `--verbose` shows full `go test -v` output
- [ ] `--json` outputs structured test results for CI consumption
- [ ] `--watch` reruns tests on file changes with debouncing
- [ ] `--cover` shows coverage per package and total
- [ ] `--cover-html` generates browsable HTML coverage report
- [ ] `--cover-min 80` fails the run if coverage is below 80%
- [ ] `--bench` runs benchmarks with optional pattern matching
- [ ] `--benchmem` includes memory allocation stats
- [ ] `--race` enables Go's race detector
- [ ] `--run TestName` filters tests by pattern
- [ ] Test database is set up automatically (SQLite in-memory by default)
- [ ] PostgreSQL test database is created, migrated, and cleaned up
- [ ] Test fixtures from `fixtures/` are loaded into the test database
- [ ] Environment variables (`GOFASTR_TEST`, `GOFASTR_DB_URL`, etc.) are set
- [ ] Exit code is 0 for pass, 1 for failures, 2 for infrastructure errors
- [ ] Watch mode prints change events and test results on each rerun
- [ ] All tests pass: `go test ./...`

---

## Implementation Notes

- Use `os/exec.Command("go", "test", ...)` to invoke `go test`. Don't reimplement the test runner.
- Parse `go test -json` output (Go 1.19+ structured JSON output) for reliable result parsing.
- For the watch mode, reuse the same file watching infrastructure as `gofastr dev` (task 039).
- Coverage parsing: use `go tool cover -func=coverage.out` to get per-function coverage, then aggregate.
- The test command should work without a config file if the test harness uses sensible defaults (SQLite in-memory).
- Consider supporting `gotestsum` as an alternative output formatter if available.
- For fixture loading, support JSON and YAML formats. Load in dependency order (e.g., users before posts).
- Test timeouts should be generous — slow CI machines need more time. Default 5m is a good balance.
