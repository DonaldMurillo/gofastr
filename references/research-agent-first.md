# Coding Agent First: Designing Frameworks for AI Agent Productivity

> Research document for **gofastr** — a fullstack Go framework designed from the ground up to be easy for AI coding agents (Claude Code, Cursor, Copilot, Aider, Devin) to understand, generate, debug, and modify.

---

## Table of Contents

1. [How AI Coding Agents Interact with Codebases](#1-how-ai-coding-agents-interact-with-codebases)
2. [Coding Agent First Design Principles](#2-coding-agent-first-design-principles)
3. [Framework Features That Help Agents Specifically](#3-framework-features-that-help-agents-specifically)
4. [AI Agent Integration as a Framework Feature](#4-ai-agent-integration-as-a-framework-feature)
5. [Existing Agent-Friendly Frameworks & Tools](#5-existing-agent-friendly-frameworks--tools)
6. [Go-Specific Agent Advantages](#6-go-specific-agent-advantages)
7. [Prioritized Feature List: Agent-First by Impact](#7-prioritized-feature-list-agent-first-by-impact)

---

## 1. How AI Coding Agents Interact with Codebases

### 1.1 The Agent Workflow

AI coding agents follow a predictable loop when working with a codebase:

1. **Read** — Scan files to understand structure, conventions, and patterns
2. **Plan** — Determine what changes are needed
3. **Write** — Generate or modify code
4. **Validate** — Run tests, linters, type checkers, and builds
5. **Debug** — Read error output, trace failures, iterate
6. **Commit** — Finalize changes when validation passes

Each step in this loop is a friction point. A framework designed for agents minimizes friction at every step.

### 1.2 Patterns Agents Struggle With

#### Runtime Metaprogramming and "Magic"
Agents cannot **execute** code to understand it. They rely on static analysis of source text. When behavior is defined at runtime — through decorators that modify function behavior invisibly, metaclasses that rewrite method tables, or `method_missing` hooks that intercept calls — the agent cannot determine what a piece of code actually does by reading it.

**Examples agents hate:**
- Ruby's `method_missing` and dynamic dispatch
- Python's `__getattr__`, `__init_subclass__`, metaclasses
- JavaScript Proxy objects that intercept property access
- Java Spring's `@Autowired` and component scanning
- Django's `models.ForeignKey('self')` lazy resolution
- Any framework that generates routes from database introspection

**Why it hurts:** The agent sees `user.posts` and must guess whether `posts` is a field, a computed property, a database query, an HTTP request, or a lazy-loaded relationship. In Go, `user.Posts` is always a struct field or an explicit method call — there's no ambiguity.

#### Implicit Behavior and Global State
When framework behavior depends on global configuration, environment variables with fallback chains, or implicit initialization order, agents cannot predict what will happen without simulating the entire runtime.

**Examples:**
- Flask's `app.config.from_object()` merging with `from_envvar()`
- Rails initializers that run in filesystem order with cross-dependencies
- Next.js's `next.config.js` rewriting imports at build time
- Webpack loader chains that transform code in non-obvious sequences
- Singleton patterns that hide initialization timing

#### Convention-Heavy Code Without Visible Boundaries
Rails is convention-over-configuration, which is *mostly* good for agents. But when the convention is "this class magically gains 30 methods because its name ends in `Controller`", the agent has to know the convention *and* trust it's applied correctly. The convention is invisible in the source code.

#### Deeply Nested Abstraction Layers
When a simple operation (e.g., "create a user") goes through 6 layers of middleware, service objects, repositories, and adapters — each adding behavior — the agent cannot trace the execution path by reading source code alone. It must hold the entire abstraction stack in context.

#### Non-Standard File Organization
Agents learn patterns from the files they read. When a project doesn't follow standard conventions (Go's `pkg/`, `cmd/`, `internal/` layout, or Rails' `app/models/`, `app/controllers/`), the agent must spend more context window figuring out where things are.

### 1.3 Patterns Agents Excel With

#### Explicit, Linear Code
Code where control flow is visible in the source — no hidden callbacks, no implicit middleware chains, no "behind the scenes" event systems. Go excels here: errors are returned values, not exceptions thrown through 10 stack frames.

```go
// Agent-friendly: explicit, linear, all paths visible
user, err := db.GetUser(ctx, id)
if err != nil {
    return fmt.Errorf("get user %d: %w", id, err)
}
if !user.Active {
    return ErrUserInactive
}
return renderUser(user)
```

```python
# Agent-hostile: multiple implicit behaviors
@require_active
@inject_db
@cache(ttl=300)
@rate_limit(key="user:{id}")
async def get_user(user: User = Depends(get_current_user)):
    return user  # What just happened? 4 decorators, any could fail
```

#### Strong Type Systems with Compile-Time Checking
When the type checker catches errors before runtime, the agent's feedback loop shortens dramatically. Instead of:
1. Write code → run app → navigate to page → trigger error → read stack trace → fix
2. The agent gets: write code → `go build` → immediate type error on line 42 → fix

This is a 10x reduction in loop time.

#### Clear File Boundaries and Single-Responsibility Files
Agents read files one at a time. When a file does one thing (define a route handler, declare a database model, implement a service), the agent can understand it in isolation. When a file mixes routing, business logic, database access, and HTML rendering, the agent must hold more context.

#### Predictable Naming Conventions
When names follow patterns (`UserHandler`, `CreateUserRequest`, `CreateUserResponse`), the agent can generate new code by analogy. It sees `CreateUserHandler` and can confidently produce `UpdateUserHandler`, `DeleteUserHandler` without guessing.

#### Standard Library Reliance
Code that uses well-documented standard libraries (Go's `net/http`, `database/sql`, `encoding/json`) is easier for agents than code that wraps those libraries in custom abstractions. Agents have extensive training data on standard library usage.

### 1.4 How Agents Use CLI Tools

Agents interact with CLI tools as their primary validation mechanism. This is the agent's equivalent of a human's IDE diagnostics panel.

**The critical loop:**
```
Write code → Run command → Parse output → Fix errors → Repeat
```

**What agents need from CLI tools:**

| Tool | Agent Usage | What Makes It Agent-Friendly |
|------|------------|------------------------------|
| `go build` | Compile-time validation | Errors with `file:line:col: message` format |
| `go test ./...` | Run all tests | PASS/FAIL per test, clear failure messages |
| `go vet` | Static analysis | Specific issues with locations |
| `staticcheck ./...` | Advanced linting | Diagnostic codes + locations |
| `gofmt` / `goimports` | Format checking | Diff output showing what's wrong |
| `gofastr build` (hypothetical) | Framework validation | Should produce structured errors |

**What makes CLI output agent-readable:**

1. **Machine-parseable format** — `file:line:col: severity: message` is trivially parsed with regex
2. **Deterministic output** — same input always produces same output (no colors, no progress bars, no interactive prompts)
3. **Actionable messages** — "undefined: CreateUserReq" tells the agent exactly what to fix
4. **No ANSI codes** — agents should be able to pipe output directly (or use `--no-color` / `--json` flags)
5. **Exit codes** — non-zero means failure, zero means success. Agents rely on `$?`

**Anti-patterns for agent CLI usage:**
- Interactive prompts that require user input (agents hang or skip)
- Progress bars that write to stderr continuously (noise in output)
- Color codes that mangle parseable output (or require `TERM=dumb`)
- Watch mode as the only interface (agents need one-shot commands)
- Errors that only appear in logs, not in command output

### 1.5 What Makes Error Messages "Agent-Readable"

**An agent-readable error has five properties:**

1. **Location** — Exact file, line, and column where the problem exists
2. **Category** — What kind of error (type error, validation error, missing dependency, etc.)
3. **Message** — A clear, specific description of what went wrong
4. **Context** — The surrounding code or relevant values
5. **Suggestion** — What to do about it (if determinable)

**Go's compiler is already excellent for agents:**
```
./handlers/user.go:23:9: cannot use user.Name (type *string) as type string in assignment
```
This has: location (`handlers/user.go:23:9`), category (type mismatch), message, context (the types involved). It doesn't have a suggestion, but the fix is obvious from the types.

**A framework should extend this pattern to runtime errors:**
```json
{
  "code": "ROUTE_HANDLER_TYPE_MISMATCH",
  "severity": "error",
  "location": {"file": "routes/users.go", "line": 45, "col": 12},
  "message": "Route handler must return (gofastr.Response, error), got (*Response, error)",
  "context": "func GetUser(ctx context.Context) (*Response, error) { ... }",
  "suggestion": "Change return type to gofastr.Response (value, not pointer). The framework handles pointer optimization internally.",
  "docs_url": "https://gofastr.dev/docs/errors/ROUTE_HANDLER_TYPE_MISMATCH"
}
```

---

## 2. Coding Agent First Design Principles

### 2.1 Convention Over Configuration (Predictable Layout)

**Principle:** Every gofastr project follows the same directory structure. Agents can navigate any gofastr project without reading documentation.

```
myapp/
├── cmd/
│   └── server/
│       └── main.go          # Entry point, always here
├── internal/
│   ├── routes/              # Route definitions
│   │   ├── users.go
│   │   └── products.go
│   ├── handlers/            # Request handlers
│   │   ├── users.go
│   │   └── products.go
│   ├── models/              # Data models
│   │   ├── user.go
│   │   └── product.go
│   ├── middleware/           # HTTP middleware
│   │   └── auth.go
│   ├── services/            # Business logic
│   │   └── user_service.go
│   └── db/                  # Database layer
│       └── queries.go
├── migrations/              # SQL migrations, numbered
│   ├── 001_create_users.sql
│   └── 002_create_products.sql
├── templates/               # HTML templates (if SSR)
│   ├── layouts/
│   └── pages/
├── static/                  # Static assets
├── tests/                   # Integration tests
│   ├── users_test.go
│   └── products_test.go
├── gofastr.toml             # Framework config (minimal)
├── go.mod
└── go.sum
```

**Why this helps agents:**
- An agent reading any gofastr project immediately knows where routes live
- `internal/routes/` → route definitions, `internal/handlers/` → handler logic
- `migrations/` → numbered SQL files, no migration framework to learn
- `gofastr.toml` → single config file, not scattered YAML/ENV/app-config hell

### 2.2 Explicit Over Implicit

**Principle:** No behavior should be invisible. If a route handler does something, it should be visible in the handler code.

**Explicit middleware:**
```go
// Agent can see exactly what middleware wraps this route
gofastr.Get("/users/:id",
    middleware.RequireAuth(),
    middleware.RateLimit(100),
    handlers.GetUser,
)
```

**NOT:**
```python
# What middleware applies here? Agent can't tell without reading settings.py
@router.get("/users/{id}")
async def get_user(id: int):
    ...
```

**Explicit error handling:**
```go
// Agent sees every failure path
func GetUser(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
    user, err := services.GetUser(ctx, req.ID)
    if err != nil {
        if errors.Is(err, db.ErrNotFound) {
            return nil, gofastr.ErrNotFound("user", req.ID)
        }
        return nil, fmt.Errorf("get user: %w", err)
    }
    return &GetUserResponse{User: user}, nil
}
```

### 2.3 Type-Safe APIs

**Principle:** Every framework API should be fully typed. Compile-time errors catch agent mistakes immediately.

**Typed route handlers:**
```go
// Request and response types are explicit and validated at compile time
type CreateUserRequest struct {
    Name  string `json:"name" validate:"required,min=2,max=100"`
    Email string `json:"email" validate:"required,email"`
    Age   int    `json:"age" validate:"min=0,max=150"`
}

type CreateUserResponse struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    Email     string    `json:"email"`
    CreatedAt time.Time `json:"created_at"`
}

// The framework enforces this signature at compile time
func CreateUser(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
    // If this doesn't match, go build fails
    ...
}
```

**Typed routes:**
```go
// Route registration is type-checked
gofastr.Post("/users", handlers.CreateUser)
// Compile error if CreateUser doesn't match the handler signature
```

**Typed database queries:**
```go
// sqlc-style: queries are typed, not stringly-typed
user, err := db.CreateUser(ctx, db.CreateUserParams{
    Name:  req.Name,
    Email: req.Email,
})
```

### 2.4 Clear Naming

**Principle:** Function and type names should be self-documenting. An agent should understand what a function does from its name alone.

**Good naming for agents:**
```go
func CreateUser(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error)
func GetUserByID(ctx context.Context, id string) (*User, error)
func ListUsers(ctx context.Context, filter UserFilter) ([]*User, error)
func UpdateUserEmail(ctx context.Context, id string, email string) error
func DeleteUser(ctx context.Context, id string) error
```

**Bad naming for agents:**
```go
func Handle(u interface{}) (interface{}, error)   // What does this handle?
func Process(r *http.Request)                     // Process what?
func Do(ctx context.Context, args ...any) error    // Do what?
func Execute(cmd string) (any, error)             // Execute what command?
```

**Naming conventions that help agents:**
- CRUD operations: `Create`, `Get`, `List`, `Update`, `Delete` prefix
- Request/response types: `{Operation}{Resource}Request` / `{Operation}{Resource}Response`
- Middleware: descriptive names like `RequireAuth`, `RateLimit`, `LogRequests`
- Errors: `ErrNotFound`, `ErrUnauthorized`, `ErrValidation`
- Configuration: `DatabaseConfig`, `ServerConfig`, `AuthConfig`

### 2.5 Small, Composable Pieces

**Principle:** Framework functions should do one thing. Agents work better with focused, composable functions than with "god functions" that accept many options.

**Composable:**
```go
// Each function does one thing, agent can mix and match
gofastr.Get("/users",
    middleware.Chain(
        middleware.LogRequests(),
        middleware.RateLimit(100),
        middleware.RequireAuth(),
    ),
    handlers.ListUsers,
)
```

**Not composable:**
```go
// One function with 15 options — agent must understand all of them
app.Handle("/users", handlers.ListUsers, &RouteConfig{
    Auth:         true,
    RateLimit:    100,
    LogRequests:  true,
    Cache:        300,
    CORS:         true,
    AllowedRoles: []string{"admin", "user"},
    Compression:  true,
    Timeout:      30 * time.Second,
    RetryPolicy:  ...,
})
```

### 2.6 Code Generation That Produces Readable Code

**Principle:** When the framework generates code (scaffolding, migrations, types), that code should be readable, modifiable, and follow the same patterns a human (or agent) would write.

**Generated code should:**
- Have clear comments explaining what it does
- Follow the same conventions as hand-written code
- Be modifiable without breaking regeneration
- Not use "generated code" as an excuse for poor quality

**Example: generated handler scaffold**
```go
// Code generated by gofastr scaffold. Edit freely.
// Route: GET /users/:id
// Handler: GetUser

package handlers

import (
    "context"
    "fmt"

    "myapp/internal/models"
)

// GetUserRequest is the input for the GetUser handler.
type GetUserRequest struct {
    ID string `param:"id" validate:"required,uuid"`
}

// GetUserResponse is the output of the GetUser handler.
type GetUserResponse struct {
    User *models.User `json:"user"`
}

// GetUser returns a single user by ID.
func GetUser(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
    // TODO: Implement user lookup
    return nil, fmt.Errorf("GetUser not implemented")
}
```

This is code the agent can immediately understand and modify. Compare with frameworks that generate abstract base classes with 200 lines of boilerplate.

### 2.7 Default Project Structure

**Principle:** `gofastr init` should create a working project that agents can immediately understand and navigate.

```bash
$ gofastr init myapp
Created myapp/
  cmd/server/main.go       # Working entry point with hello world route
  internal/routes/routes.go # Route registration
  internal/handlers/        # Handler directory with example handler
  internal/models/          # Model directory
  internal/middleware/      # Middleware directory
  migrations/               # Empty, ready for migrations
  gofastr.toml              # Minimal config with sensible defaults
  go.mod                    # Go module initialized
  README.md                 # Generated with project-specific instructions
```

**The README.md should be agent-friendly too:**
```markdown
# myapp

Built with [gofastr](https://gofastr.dev).

## Commands
- `go run ./cmd/server` — Start development server
- `gofastr build` — Build production binary
- `gofastr test` — Run all tests
- `gofastr migrate create <name>` — Create a new migration
- `gofastr scaffold <resource>` — Scaffold CRUD for a resource
- `gofastr routes` — List all registered routes

## Structure
- `cmd/server/` — Application entry point
- `internal/routes/` — Route definitions
- `internal/handlers/` — Request handlers
- `internal/models/` — Data models
- `internal/middleware/` — HTTP middleware
- `migrations/` — Database migrations

## Quick Start
1. `cp gofastr.toml.example gofastr.toml`
2. `docker compose up -d` (starts PostgreSQL)
3. `gofastr migrate up`
4. `go run ./cmd/server`
```

### 2.8 Documentation as API Surface

**Principle:** Doc comments are part of the framework API. They must be accurate, complete, and include examples.

**Go doc comments are agent gold:**
```go
// CreateUser creates a new user in the database.
//
// It validates the request, checks for duplicate emails,
// hashes the password, and inserts the user record.
//
// Returns the created user with a generated ID, or an error if:
//   - ErrValidation: request validation failed
//   - ErrDuplicate: email already exists
//   - ErrDatabase: database operation failed
//
// Example:
//
//   resp, err := handlers.CreateUser(ctx, &handlers.CreateUserRequest{
//       Name:  "Alice",
//       Email: "alice@example.com",
//   })
//   if err != nil {
//       return err
//   }
//   fmt.Println(resp.ID) // "usr_abc123"
func CreateUser(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
```

Agents can read `go doc` output directly and get exactly this text. This is a major advantage over frameworks where the only documentation is on a website.

---

## 3. Framework Features That Help Agents Specifically

### 3.1 CLI with JSON Output Mode

Every CLI command should support `--json` or `--output json` for machine-parseable output.

```bash
# Human-readable
$ gofastr routes
GET    /users          handlers.ListUsers
POST   /users          handlers.CreateUser
GET    /users/:id      handlers.GetUser
PUT    /users/:id      handlers.UpdateUser
DELETE /users/:id      handlers.DeleteUser

# Agent-parseable
$ gofastr routes --json
[
  {"method": "GET",    "path": "/users",     "handler": "handlers.ListUsers"},
  {"method": "POST",   "path": "/users",     "handler": "handlers.CreateUser"},
  {"method": "GET",    "path": "/users/:id", "handler": "handlers.GetUser"},
  {"method": "PUT",    "path": "/users/:id", "handler": "handlers.UpdateUser"},
  {"method": "DELETE", "path": "/users/:id", "handler": "handlers.DeleteUser"}
]
```

```bash
# Errors in JSON
$ gofastr build --json
{
  "success": false,
  "errors": [
    {
      "code": "DUPLICATE_ROUTE",
      "severity": "error",
      "location": {"file": "internal/routes/users.go", "line": 23},
      "message": "Route GET /users/:id is defined twice",
      "suggestion": "Rename or remove the duplicate route in routes/users.go:23"
    }
  ]
}
```

### 3.2 Scaffold/Generate Commands

Agents need to be able to create boilerplate without understanding every internal detail. Scaffold commands should produce the same code an experienced human would write.

```bash
# Scaffold a complete CRUD resource
$ gofastr scaffold user name:string email:string age:int

# Generates:
#   internal/models/user.go        — Model with fields
#   internal/handlers/users.go      — CRUD handlers (Create, Get, List, Update, Delete)
#   internal/routes/users.go        — Route registration
#   internal/services/user_service.go — Business logic stubs
#   migrations/NNN_create_users.sql — Migration with table creation
#   tests/users_test.go             — Test file with example tests

# Scaffold individual pieces
$ gofastr generate handler users:GetUser
$ gofastr generate migration add_users_email_index
$ gofastr generate middleware ratelimit
$ gofastr generate model product name:string price:float64
```

### 3.3 Built-in Lint/Format for Agent Mistakes

A framework-specific linter that catches common mistakes agents make:

```bash
$ gofastr lint
internal/handlers/users.go:15: HANDLER_SIG: Handler "GetUser" must accept (*GetUserRequest) as second parameter, got (*GetUserReq)
internal/routes/routes.go:8: MISSING_HANDLER: Route references handlers.ListUsrs which does not exist (did you mean handlers.ListUsers?)
internal/handlers/users.go:23: UNHANDLED_ERROR: Error from db.GetUser is not checked
internal/middleware/auth.go:12: CONTEXT_VALUE: Use gofastr.GetUserID(ctx) instead of ctx.Value("user_id")
```

These are errors that `go vet` wouldn't catch but that are specific to framework conventions.

### 3.4 Structured Error Output

Framework errors at runtime should be structured for machine parsing:

```go
// Development mode: returns JSON error responses
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Request validation failed",
    "details": [
      {
        "field": "email",
        "rule": "email",
        "message": "must be a valid email address",
        "value": "not-an-email"
      },
      {
        "field": "age",
        "rule": "min",
        "message": "must be at least 0",
        "value": -5
      }
    ],
    "request_id": "req_abc123",
    "docs": "https://gofastr.dev/docs/errors/VALIDATION_ERROR"
  }
}
```

### 3.5 Type-Safe Routing

Routes should be validated at compile time, not runtime:

```go
// Approach 1: Code generation from route definitions
// gofastr generate routes reads route definitions and generates typed route functions

// Approach 2: Generic-based type safety
gofastr.Get("/users/:id",
    gofastr.Typed(handlers.GetUser), // Compile-time check that GetUser matches handler signature
)

// Approach 3: Interface-based type safety
type Handler[Req any, Resp any] func(ctx context.Context, req *Req) (*Resp, error)

gofastr.Handle("/users/:id", Handler[GetUserRequest, GetUserResponse](handlers.GetUser))
```

### 3.6 Schema-First API Design

The framework should encourage defining schemas first, then generating code:

```go
// internal/models/user.go — the schema is the source of truth
type User struct {
    ID        string    `json:"id" db:"id" param:"id"`
    Name      string    `json:"name" db:"name" validate:"required,min=2,max=100"`
    Email     string    `json:"email" db:"email" validate:"required,email"`
    Age       int       `json:"age" db:"age" validate:"min=0,max=150"`
    CreatedAt time.Time `json:"created_at" db:"created_at"`
    UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}
```

From this single definition, the framework can derive:
- JSON serialization/deserialization
- Database column mapping
- Request validation rules
- OpenAPI schema generation
- Extension-driven frontend/client code generation
- Migration generation

### 3.7 Built-in Testing Patterns

Test helpers that make it easy for agents to write tests:

```go
package tests

import (
    "testing"
    "myapp/internal/handlers"
    "myapp/internal/testutil"
)

func TestCreateUser(t *testing.T) {
    // Framework provides test utilities
    app := testutil.NewTestApp(t) // Sets up test DB, config, etc.
    defer app.Cleanup()

    // Easy request making
    resp := app.Post("/users", handlers.CreateUserRequest{
        Name:  "Alice",
        Email: "alice@example.com",
    })

    // Framework provides assertions
    resp.AssertOK()
    resp.AssertJSON(t, handlers.CreateUserResponse{
        Name:  "Alice",
        Email: "alice@example.com",
    })

    // Can also check database state
    app.AssertDBHas(t, "users", map[string]any{
        "email": "alice@example.com",
    })
}
```

### 3.8 Configuration Validation with Clear Errors

```bash
$ go run ./cmd/server

Configuration Error: gofastr.toml
  Line 12: database.url is required but not set
  Line 15: server.port must be between 1 and 65535, got "abc"
  Line 18: auth.secret must be at least 32 characters, got 10

Suggestion: Copy gofastr.toml.example and fill in the values:
  cp gofastr.toml.example gofastr.toml

Documentation: https://gofastr.dev/docs/configuration
```

### 3.9 Hot Reload with Parseable Error Output

```bash
$ gofastr dev
2026-05-05T10:30:00Z INFO Server started on :8080
2026-05-05T10:30:00Z INFO Watching for file changes...

# File change triggers rebuild, error goes to stderr in parseable format
2026-05-05T10:30:15Z ERROR Build failed
  internal/handlers/users.go:23: undefined: CreateUserReq (did you mean CreateUserRequest?)
  internal/handlers/users.go:28: cannot use 42 (type int) as type string in field Value

# Fix applied, rebuild succeeds
2026-05-05T10:30:22Z INFO Rebuild successful, reloading server
2026-05-05T10:30:22Z INFO Server restarted on :8080
```

---

## 4. AI Agent Integration as a Framework Feature

### 4.1 Built-in MCP (Model Context Protocol) Server

The Model Context Protocol is an open standard (by Anthropic) for connecting AI assistants to external tools and data sources. A built-in MCP server makes gofastr immediately usable by any MCP-compatible agent (Claude Code, Cursor, etc.).

**What the MCP server would expose:**

```json
{
  "tools": [
    {
      "name": "gofastr_list_routes",
      "description": "List all registered routes in the project",
      "inputSchema": { "type": "object", "properties": {} }
    },
    {
      "name": "gofastr_list_models",
      "description": "List all data models and their fields",
      "inputSchema": { "type": "object", "properties": {} }
    },
    {
      "name": "gofastr_scaffold",
      "description": "Scaffold a new resource (model, handler, routes, migration)",
      "inputSchema": {
        "type": "object",
        "properties": {
          "resource": { "type": "string", "description": "Resource name (e.g., 'user', 'product')" },
          "fields": { "type": "array", "items": { "type": "string" }, "description": "Fields as name:type pairs" }
        }
      }
    },
    {
      "name": "gofastr_run_migrations",
      "description": "Run pending database migrations",
      "inputSchema": { "type": "object", "properties": { "direction": { "enum": ["up", "down"] } } }
    },
    {
      "name": "gofastr_validate",
      "description": "Validate the project configuration, routes, and types",
      "inputSchema": { "type": "object", "properties": {} }
    },
    {
      "name": "gofastr_generate_openapi",
      "description": "Generate OpenAPI 3.0 spec from routes and types",
      "inputSchema": { "type": "object", "properties": {} }
    },
    {
      "name": "gofastr_explain_error",
      "description": "Given an error message, explain what it means and suggest fixes",
      "inputSchema": {
        "type": "object",
        "properties": {
          "error": { "type": "string", "description": "The error message to explain" }
        }
      }
    }
  ],
  "resources": [
    {
      "uri": "gofastr://config",
      "description": "Current framework configuration"
    },
    {
      "uri": "gofastr://routes",
      "description": "All registered routes with handlers"
    },
    {
      "uri": "gofastr://models",
      "description": "All data models with field definitions"
    },
    {
      "uri": "gofastr://middleware",
      "description": "Registered middleware and their order"
    },
    {
      "uri": "gofastr://docs/{topic}",
      "description": "Framework documentation for a specific topic"
    }
  ]
}
```

**Implementation:** The MCP server runs as a subprocess (stdio transport) that Claude Code or Cursor can launch. It reads the project's source files and configuration to provide real-time context.

### 4.2 Tool Calling Integration

For applications built with gofastr, the framework can expose structured actions that AI agents can call:

```go
// Define AI-callable actions in your app
gofastr.AIAction("search_products", "Search the product catalog",
    func(ctx context.Context, req *SearchProductsRequest) (*SearchProductsResponse, error) {
        return services.SearchProducts(ctx, req.Query, req.Filters)
    },
)

gofastr.AIAction("get_order_status", "Get the status of an order",
    func(ctx context.Context, req *GetOrderStatusRequest) (*GetOrderStatusResponse, error) {
        return services.GetOrderStatus(ctx, req.OrderID)
    },
)
```

The framework auto-generates:
- Tool definitions for MCP/OpenAI function calling
- Input validation schemas
- Type-safe request/response handling
- Permission checking (which actions require auth)

### 4.3 Structured Logging for AI Reasoning

Logs that are easy for both humans and AI to parse:

```go
// Structured logging with gofastr
logger.Info("user_created",
    "user_id", user.ID,
    "email", user.Email,
    "source", "signup",
)

// Output (JSON mode for agents):
{"level":"info","time":"2026-05-05T10:30:00Z","msg":"user_created","user_id":"usr_abc","email":"alice@example.com","source":"signup"}
```

**Log conventions that help agents:**
- Every log line has a structured message type (not free-form text)
- Key-value pairs use consistent naming (`user_id`, not sometimes `userId` and sometimes `uid`)
- Request IDs are propagated through all log lines
- Errors include stack traces in development mode

### 4.4 Auto-Generated API Documentation

```go
// From route definitions and types, generate OpenAPI spec
// $ gofastr docs generate
// Produces: openapi.json, openapi.yaml

// Project-specific frontend type artifacts should now be added through a
// configured codegen extension rather than a built-in generator.
export interface CreateUserRequest {
  name: string;
  email: string;
  age: number;
}

export interface CreateUserResponse {
  id: string;
  name: string;
  email: string;
  created_at: string;
}
```

### 4.5 Built-in AI Endpoints

For apps that need AI features, gofastr can provide:

```go
// Built-in chat/search endpoint for RAG-style AI features
gofastr.AIChat("/api/chat",
    gofastr.ChatConfig{
        SystemPrompt: "You are a helpful assistant for {{.AppName}}",
        ContextFunc:  contextFunc, // Provides relevant context
    },
)

// Built-in search endpoint using embedding search
gofastr.AISearch("/api/search",
    gofastr.SearchConfig{
        EmbeddingFunc: embeddings.OpenAI("text-embedding-3-small"),
        VectorStore:   vectorstore.Postgres(db),
    },
)
```

### 4.6 Prompt-Friendly Error Pages

In development mode, error pages should include context that AI agents can copy and use:

```
╔══════════════════════════════════════════════════════════════╗
║  500 Internal Server Error                                   ║
╠══════════════════════════════════════════════════════════════╣
║                                                              ║
║  Error: database connection refused                           ║
║  File: internal/services/user_service.go:42                  ║
║  Handler: handlers.ListUsers                                 ║
║  Route: GET /users                                           ║
║  Request ID: req_abc123                                      ║
║                                                              ║
║  Probable cause: PostgreSQL is not running                   ║
║  Suggestion: docker compose up -d                            ║
║                                                              ║
║  Stack trace:                                                ║
║    internal/services/user_service.go:42                      ║
║    internal/handlers/users.go:28                             ║
║    gofastr/router.go:156                                     ║
║                                                              ║
║  AI Debug Context (copy this to your AI assistant):          ║
║  ┌──────────────────────────────────────────────────────────┐║
║  │ Error in gofastr handler handlers.ListUsers at           │║
║  │ GET /users. Database connection refused at               │║
║  │ internal/services/user_service.go:42. Check that         │║
║  │ the database configured in gofastr.toml is running.      │║
║  │ Run: docker compose up -d                                │║
║  └──────────────────────────────────────────────────────────┘║
╚══════════════════════════════════════════════════════════════╝
```

---

## 5. Existing Agent-Friendly Frameworks & Tools

### 5.1 Rails: Convention Over Configuration as Agent Superpower

**What Rails gets right for agents:**
- **Predictable file layout:** `app/models/user.rb`, `app/controllers/users_controller.rb`, `app/views/users/index.html.erb` — always the same
- **Naming conventions:** `User` model → `users` table, `UsersController` → `/users` routes
- **Standard CRUD patterns:** `index`, `show`, `new`, `create`, `edit`, `update`, `destroy` — seven actions every controller can implement

**What Rails gets wrong for agents:**
- **Implicit behavior:** `before_action :authenticate_user!` — which controller actions does this apply to? Only implicit (all of them, unless `only:` / `except:` is specified)
- **Dynamic finder methods:** `User.find_by_email_and_active(email, true)` — generated at runtime, invisible in source
- **Concerns and mixins:** `include Authenticatable` — what methods does this add? Agent must read the concern AND understand the include order
- **Gems that monkey-patch:** `acts_as_paranoid` redefines `.destroy` — agent can't see this from reading the model

**Lesson for gofastr:** Adopt Rails' convention-over-configuration philosophy, but make all behavior explicit in source code. No "magic" that isn't visible.

### 5.2 Go's Explicit Error Handling

**Why Go is ideal for agents:**

```go
// Every error path is visible
result, err := doSomething()
if err != nil {
    return fmt.Errorf("doing something: %w", err)
}
```

- **No hidden exceptions:** Unlike Python/Java, you can't have an unexpected `NullPointerException` from 5 levels deep. Every error is returned and handled explicitly.
- **Error wrapping:** `fmt.Errorf("context: %w", err)` creates a chain that's traceable
- **errors.Is / errors.As:** Agents can understand error type checking without understanding inheritance hierarchies

**The explicit error pattern means:**
1. Agent writes code
2. `go build` catches type errors
3. `go test` catches logic errors
4. Error messages point to exact files and lines
5. Agent reads error, fixes code, repeats

Compare with JavaScript where an error might be `TypeError: Cannot read property 'id' of undefined` with a 50-frame stack trace through transpiled code.

### 5.3 Next.js: File-Based Routing

**What Next.js gets right for agents:**
- **File system is the API:** `app/users/page.tsx` → `/users` route. Agent can create a route by creating a file.
- **Colocation:** `page.tsx`, `loading.tsx`, `error.tsx`, `layout.tsx` all in the same directory
- **Convention is structure:** No central router file to maintain; the file system IS the routing configuration

**What Next.js gets wrong for agents:**
- **Server vs client components:** `"use client"` directive is easy to miss, and the behavior difference is invisible
- **Implicit data fetching:** `async function Page()` that directly awaits — agent must know this is server-only
- **Build-time optimizations:** Next.js rewrites imports, splits bundles, and pre-renders in ways invisible in source
- **Complex configuration:** `next.config.js` with rewrites, redirects, headers, middleware — growing complexity

**Lesson for gofastr:** File-based conventions are great, but the framework should have a single, explicit route registration file that serves as the source of truth.

### 5.4 tRPC: Type Safety as Agent Safety Net

**What tRPC gets right for agents:**
- **End-to-end type safety:** A typo in a procedure name is a compile error, not a runtime 404
- **Procedure definitions are self-documenting:**
  ```typescript
  const userRouter = t.router({
    getUser: t.procedure.input(z.object({ id: z.string() })).query(({ input }) => {
      // Agent knows: input is { id: string }, returns User type
    }),
  });
  ```
- **Auto-generated client types:** Frontend can't call the wrong endpoint
- **Inferrable from code:** Agent can understand the full API by reading the router file

**What tRPC gets wrong for agents:**
- **Zod runtime validation:** The schema is defined at runtime, not compile-time. Agent must understand the Zod API
- **Complex inference chains:** TypeScript's type inference for tRPC routers can produce incomprehensible error messages
- **Tight coupling to TypeScript:** Agent must be fluent in advanced TypeScript generics

**Lesson for gofastr:** Go's type system is simpler than TypeScript's. Use Go generics for type-safe handlers but keep the type signatures simple and explicit.

### 5.5 What AI Agents Struggle With in Existing Frameworks

**Claude Code struggles with:**
- Large monorepos with thousands of files (context window limits)
- Complex build systems (webpack, vite, turborepo) where the relationship between source and output is unclear
- Frameworks with many configuration files (Next.js has `next.config.js`, `tsconfig.json`, `tailwind.config.js`, `postcss.config.js`, `.env.local`, etc.)
- Error messages that reference compiled/bundled code instead of source code

**Cursor struggles with:**
- Understanding cross-file dependencies in large codebases
- Frameworks where the "right" way to do something changed between versions (e.g., Next.js 13 vs 14 vs 15)
- Implicit behavior that requires reading framework source code to understand
- Test frameworks that require specific setup patterns (Jest config, testing library setup)

**Aider struggles with:**
- Files larger than what fits in context — it must chunk, losing global view
- Non-text files (images, binary assets, migrations)
- Complex git states (merge conflicts, rebases)
- Frameworks that require coordinated changes across many files

**Common patterns across all agents:**
1. **Context window pressure:** Every file the agent reads is a cost. Frameworks that require reading many files to understand one feature are expensive.
2. **Feedback loop length:** The faster an agent can validate its code (compile, test, lint), the more productive it is.
3. **Error message quality:** Vague errors ("something went wrong") vs specific errors ("file X, line Y: type mismatch") directly impact agent iteration speed.
4. **Convention knowledge:** Agents that know the conventions can generate correct code on the first try. Frameworks with strong conventions are faster for agents.

---

## 6. Go-Specific Agent Advantages

### 6.1 Tooling Agents Can Run Directly

Go's toolchain is uniquely agent-friendly because every tool:
- Produces deterministic output
- Uses the `file:line:col: message` format
- Returns meaningful exit codes
- Requires zero interactive input
- Is installed with the Go toolchain (no separate installs needed)

```bash
# Agent validation loop — all of these are one-shot commands

# 1. Format check
$ gofmt -l .
internal/handlers/users.go    # Lists files that need formatting

# 2. Import management
$ goimports -l .
# Auto-fixes imports

# 3. Compile check (fastest feedback)
$ go build ./...
# Errors with file:line:col: message

# 4. Static analysis
$ go vet ./...
# Catches common mistakes

# 5. Advanced linting
$ staticcheck ./...
# Detailed diagnostics with codes

# 6. Test run
$ go test ./... -v -json
# JSON-formatted test output

# 7. Documentation
$ go doc gofastr.Handler
# Shows documentation for any type or function

# 8. Dependency analysis
$ go mod tidy
# Cleans up dependencies

# 9. Race detector
$ go test -race ./...
# Detects race conditions
```

### 6.2 Compile-Time Type Checking: The Agent's Best Friend

Go's compiler is the single biggest advantage for agents. Consider the difference:

**Python (agent writes code):**
1. Agent writes `user.nmae` (typo)
2. No compile error
3. Agent runs tests — maybe the test doesn't cover this path
4. Agent commits
5. Bug reaches production

**Go (agent writes code):**
1. Agent writes `user.Nmae` (typo)
2. `go build` immediately: `./handlers.go:23: user.Nmae undefined (type User has no field or method Nmae)`
3. Agent reads error, fixes to `user.Name`
4. Build succeeds
5. Correct code committed

**The type checker catches:**
- Undefined fields and methods
- Wrong argument types
- Missing return values
- Incorrect interface implementations
- Unused imports and variables
- Shadowing issues

### 6.3 Go Test: Standard Testing Agents Can Rely On

Go's built-in testing framework means agents always know how to write tests:

```go
// Every Go test follows this exact pattern
func TestXxx(t *testing.T) {
    // Setup
    input := ...

    // Execute
    result, err := functionUnderTest(input)

    // Assert
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if result != expected {
        t.Errorf("got %v, want %v", result, expected)
    }
}
```

**Agent-friendly properties of Go tests:**
- `_test.go` suffix — agent knows where tests live
- `func TestXxx(t *testing.T)` — agent knows the signature
- `go test ./...` — runs all tests, no configuration needed
- `-v` flag for verbose output
- `-run TestName` to run specific tests
- `-json` flag for parseable output
- Table-driven tests are a standard pattern agents learn
- Testify or standard library — both are well-known to agents

### 6.4 Go Doc: Programmable Documentation

```bash
$ go doc gofastr.Handler
package gofastr -- gofastr

type Handler[Req any, Resp any] func(ctx context.Context, req *Req) (*Resp, error)

Handler is the standard function signature for route handlers.

The framework validates the request body against Req at runtime
and serializes the Resp as JSON.

Req must be a struct with json tags for deserialization.
Resp must be a struct with json tags for serialization.

Example:

    func GetUser(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
        user, err := db.GetUser(ctx, req.ID)
        if err != nil {
            return nil, fmt.Errorf("get user: %w", err)
        }
        return &GetUserResponse{User: user}, nil
    }
```

**Agent workflow:**
1. Agent needs to create a handler
2. Agent runs `go doc gofastr.Handler`
3. Agent sees the exact signature, constraints, and example
4. Agent writes correct code on first try

### 6.5 Explicit Error Handling: Agents Always See What Can Fail

In Go, every function that can fail returns an `error`. This means:
- Agents can see every failure point by reading function signatures
- Agents know they must handle errors (compiler warns about unused variables)
- Error chains make debugging traceable

```go
// Agent can see: this function can fail
func CreateUser(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
    // Agent can see: Validate can fail
    if err := validate(req); err != nil {
        return nil, gofastr.ErrValidation(err)
    }
    // Agent can see: db.Insert can fail
    user, err := db.InsertUser(ctx, &db.User{
        Name:  req.Name,
        Email: req.Email,
    })
    if err != nil {
        // Agent can see: need to handle duplicate email
        if errors.Is(err, db.ErrUniqueViolation) {
            return nil, gofastr.ErrConflict("email already exists")
        }
        return nil, fmt.Errorf("insert user: %w", err)
    }
    return &CreateUserResponse{ID: user.ID}, nil
}
```

Compare with Python where `User.objects.create(...)` could raise `IntegrityError`, `ValidationError`, `DatabaseError`, or any number of other exceptions that are not visible in the function signature.

### 6.6 No Inheritance, No Magic: Composition Agents Can Reason About

Go has no class inheritance. This is a feature for agents:

```go
// Composition: agent can see exactly what's included
type UserService struct {
    db     *db.Queries
    logger *slog.Logger
    cache  *cache.Cache
}

func (s *UserService) GetUser(ctx context.Context, id string) (*User, error) {
    // Agent sees: uses db, logger, cache — all explicit
    cached, ok := s.cache.Get("user:" + id)
    if ok {
        return cached.(*User), nil
    }
    user, err := s.db.GetUser(ctx, id)
    if err != nil {
        s.logger.Error("get user failed", "id", id, "error", err)
        return nil, err
    }
    s.cache.Set("user:"+id, user, 5*time.Minute)
    return user, nil
}
```

**No hidden behavior:**
- No virtual method dispatch to reason about
- No abstract base classes with template methods
- No `super()` calls that might do anything
- No mixins that add methods invisibly
- Interfaces are satisfied implicitly but behavior is always explicit

---

## 7. Prioritized Feature List: Agent-First by Impact

Each feature is ranked by **impact on agent productivity** (how much it speeds up the agent's write→validate→fix loop) with **implementation complexity** notes.

### Tier 1: Critical (Massive Agent Productivity Impact)

| # | Feature | Impact | Complexity | Notes |
|---|---------|--------|------------|-------|
| 1 | **Go's existing type system** | 🔴 Critical | ✅ Free | Go already provides this. Lean into it. Every handler should be fully typed. |
| 2 | **Standard project layout** | 🔴 Critical | 🟢 Low | Define and enforce a directory convention. Agents navigate by convention. |
| 3 | **Clear file:line:col error messages** | 🔴 Critical | 🟢 Low | Follow Go compiler conventions. Every error should have location info. |
| 4 | **`go build` as validation** | 🔴 Critical | ✅ Free | If the framework compiles, it's probably correct. Design APIs that don't compile when misused. |
| 5 | **Scaffold commands** | 🔴 Critical | 🟡 Medium | `gofastr scaffold user` generates full CRUD. Agents invoke this instead of writing boilerplate. |
| 6 | **Typed request/response structs** | 🔴 Critical | 🟢 Low | Standard Go pattern. Framework validates based on struct tags. |

### Tier 2: High Impact (Significant Agent Speedup)

| # | Feature | Impact | Complexity | Notes |
|---|---------|--------|------------|-------|
| 7 | **CLI with `--json` flag** | 🟠 High | 🟡 Medium | Every command produces parseable JSON. Agents read JSON better than formatted text. |
| 8 | **Test helpers and fixtures** | 🟠 High | 🟡 Medium | `testutil.NewTestApp(t)`, `app.Post()`, `resp.AssertOK()`. Makes agent-written tests consistent. |
| 9 | **Framework-specific linter** | 🟠 High | 🔴 High | Custom `go vet` pass or linter that catches framework-specific mistakes. Huge value but significant effort. |
| 10 | **Configuration validation** | 🟠 High | 🟢 Low | Fail fast with clear messages on bad config. `gofastr validate` command. |
| 11 | **Auto-generated OpenAPI spec** | 🟠 High | 🟡 Medium | Derive OpenAPI from Go types. Agents can validate API contracts. |
| 12 | **Hot reload with structured errors** | 🟠 High | 🟡 Medium | `gofastr dev` watches files, rebuilds, reports errors to stderr in parseable format. |

### Tier 3: Medium Impact (Nice Agent Experience)

| # | Feature | Impact | Complexity | Notes |
|---|---------|--------|------------|-------|
| 13 | **MCP server** | 🟡 Medium | 🔴 High | Built-in MCP server for Claude Code / Cursor integration. Powerful but complex to build. |
| 14 | **Structured error responses** | 🟡 Medium | 🟢 Low | Runtime errors include code, field, message, suggestion in JSON. |
| 15 | **Extension-driven client codegen** | 🟡 Medium | 🟡 Medium | Let configured generators emit frontend or client artifacts from stable source data. |
| 16 | **Prompt-friendly error pages** | 🟡 Medium | 🟢 Low | Dev mode shows copy-pasteable error context for AI assistants. |
| 17 | **`gofastr explain` command** | 🟡 Medium | 🟡 Medium | Given an error code, explain what it means with examples. |
| 18 | **Schema-first model definitions** | 🟡 Medium | 🟢 Low | Struct tags define validation, DB mapping, JSON serialization. Single source of truth. |

### Tier 4: Lower Impact (Future Enhancements)

| # | Feature | Impact | Complexity | Notes |
|---|---------|--------|------------|-------|
| 19 | **AI action integration** | 🔵 Low | 🟡 Medium | Define AI-callable actions for apps built with gofastr. Nice for AI-enabled apps. |
| 20 | **Built-in AI endpoints** | 🔵 Low | 🔴 High | Chat/search endpoints. Useful for app developers but not agent productivity. |
| 21 | **Structured logging conventions** | 🔵 Low | 🟢 Low | Standardize log format. Agents can parse logs when debugging. |
| 22 | **Built-in AI chat/search** | 🔵 Low | 🔴 High | For apps, not for agent productivity. |
| 23 | **Migration generation** | 🔵 Low | 🟡 Medium | Auto-generate migrations from model changes. Useful but agents can write SQL. |

### Implementation Priority (Recommended Build Order)

**Phase 1 — Foundation (Week 1-2):**
1. Standard project layout + `gofastr init`
2. Typed handler signatures + route registration
3. Clear error messages following Go conventions
4. `gofastr scaffold` commands

**Phase 2 — Validation (Week 3-4):**
5. `--json` flags on all CLI commands
6. Test helper library
7. Configuration validation
8. Hot reload with structured error output

**Phase 3 — Developer Experience (Week 5-8):**
9. Auto-generated OpenAPI spec
10. Framework-specific linter (start with `go vet` pass)
11. Structured error responses in dev mode
12. Extension-driven client codegen

**Phase 4 — AI Integration (Week 9-12):**
13. MCP server
14. `gofastr explain` command
15. AI action definitions
16. Prompt-friendly error pages

---

## Appendix: The Agent Feedback Loop

The single most important concept in agent-first design is the **feedback loop**:

```
┌─────────────────────────────────────────────────────────┐
│                    AGENT FEEDBACK LOOP                    │
│                                                          │
│  Write ──→ Build ──→ Test ──→ Lint ──→ Run ──→ Debug   │
│    ▲                                          │          │
│    └──────────── Fix based on output ─────────┘          │
│                                                          │
│  Each step should be:                                    │
│  • Fast (seconds, not minutes)                           │
│  • Deterministic (same input → same output)              │
│  • Actionable (tells agent exactly what to fix)           │
│  • Parseable (file:line:col format)                      │
│                                                          │
│  Go's toolchain already provides this.                   │
│  The framework's job is to NOT break this loop.          │
└─────────────────────────────────────────────────────────┘
```

**The framework's golden rule:** Every feature added should either speed up this loop or stay out of its way. Features that add hidden behavior, slow builds, or produce vague errors directly harm agent productivity.

---

*Research compiled for the gofastr project — a coding agent first fullstack Go framework.*
*Date: 2026-05-05*
