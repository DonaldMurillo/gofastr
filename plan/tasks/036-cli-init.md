# Task 036: CLI Init Command

**Phase:** 4 — CLI & DX  
**Depends on:** 035 (CLI Framework)  
**Status:** not started

---

## Goal

Implement `gofastr init [name]` — the command that scaffolds a new GoFastr project. This is the first command a developer or AI agent runs. It must work both interactively (human) and non-interactively (AI agent with `--defaults` or `--json`).

---

## Context

From the draft:

> ```
> gofastr init "blog with posts and tags"   → scaffold project + entities
> ```

From the proposal:

> AI agents generate entity declarations (Go or JSON). The framework compiles them into a working webapp.

The init command creates the project structure that all other commands expect. It must generate a working "hello world" GoFastr app that can be immediately built and run.

---

## Requirements

### 1. Command Definition

```
gofastr init [name] [flags]
```

- `name` is optional. If not provided:
  - **Interactive mode:** prompt for project name.
  - **Non-interactive (`--json` or `--defaults`):** error if name not provided.

### 2. Flags

| Flag | Type | Default | Purpose |
|------|------|---------|---------|
| `--defaults` / `-d` | bool | false | Use all defaults, no prompts. For AI agents and scripts. |
| `--description` | string | `""` | Project description (skips prompt). |
| `--module` | string | auto | Go module path. Default: `github.com/user/<name>`. |
| `--db` | string | `"sqlite"` | Database driver: `sqlite`, `postgres`. |
| `--features` | stringSlice | `[]` | Features to include: `auth`, `mcp`, `static`, `upload`. |
| `--no-git` | bool | false | Skip git initialization. |

### 3. Interactive Mode (default, no `--json` and no `--defaults`)

Prompt the user using a survey library (e.g., [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) or [AlecAivazis/survey](https://github.com/AlecAivazis/survey)):

1. **Project name** — validate: lowercase, hyphens ok, no spaces. Default: directory name.
2. **Description** — optional text. Default: empty.
3. **Module path** — default: `github.com/user/<name>`.
4. **Database** — select from: `sqlite` (default), `postgres`.
5. **Features** — multi-select: `auth`, `mcp`, `static`, `upload`. Default: all selected.

If `--json` is set, skip all prompts (fall back to defaults or explicit flags).

### 4. Generated Project Structure

```
<name>/
├── go.mod
├── go.sum                      # after go mod tidy
├── main.go                     # minimal working app
├── gofastr.yaml                # project config
├── .gitignore
├── .gofastr/                   # generated code (gitignored)
│   └── .gitkeep
├── entities/
│   └── example.json            # example entity to get started
├── static/
│   └── favicon.ico             # placeholder
├── templates/                  # optional, if rendering feature
│   └── .gitkeep
└── migrations/
    └── .gitkeep
```

### 5. File Templates

#### `go.mod`

```
module <module-path>

go 1.22

require github.com/DonaldMurillo/gofastr v0.1.0
```

(Use the actual module path and version.)

#### `main.go`

```go
package main

import (
    "github.com/DonaldMurillo/gofastr"
    _ "<module-path>/entities"  // if using Go entity files
)

func main() {
    app := gofastr.New("<name>")
    
    // Load entities from JSON files
    app.EntitiesFromDir("entities/")
    
    app.Run()
}
```

#### `gofastr.yaml`

```yaml
name: <name>
version: "0.1.0"
db:
  driver: <db-driver>
  url: "<db-url>"  # e.g., "file:gofastr.db" for sqlite
server:
  port: 8080
  host: "localhost"
dev:
  port: 3000
  open_browser: true
  watch_paths:
    - "."
    - "entities/"
    - "templates/"
    - "static/"
  ignore_paths:
    - ".gofastr/"
    - "node_modules/"
    - "vendor/"
```

#### `entities/example.json`

```json
{
  "name": "posts",
  "fields": [
    { "name": "title", "type": "string", "required": true, "max": 200 },
    { "name": "body", "type": "text" },
    { "name": "published", "type": "bool", "default": false }
  ],
  "crud": true,
  "mcp": true
}
```

#### `.gitignore`

```
.gofastr/
*.db
.env
```

### 6. Go Module Initialization

1. Create project directory (if name != current directory name).
2. `cd` into it.
3. Run `go mod init <module-path>`.
4. Run `go mod tidy` (after writing `main.go` with imports).
5. If `gofastr` isn't available as a real module yet, use a `replace` directive in `go.mod` pointing to the local development path.

### 7. Git Initialization

1. Run `git init` in the project directory.
2. Create initial commit: `git add . && git commit -m "Initialize GoFastr project"`.

### 8. Output

#### Human Mode

```
✓ Created project <name> in ./<name>/

  Generated:
    main.go           — Application entry point
    gofastr.yaml      — Project configuration
    entities/example.json — Example entity (posts)
  
  Next steps:
    cd <name>
    gofastr dev       — Start development server with hot-reload
    gofastr build     — Build production binary

  Docs: https://gofastr.dev/docs/getting-started
```

#### JSON Mode (`--json`)

```json
{
  "status": "success",
  "data": {
    "name": "myapp",
    "path": "/path/to/myapp",
    "module": "github.com/user/myapp",
    "files_created": [
      "go.mod",
      "main.go",
      "gofastr.yaml",
      ".gitignore",
      "entities/example.json",
      "static/favicon.ico"
    ],
    "next_steps": [
      "cd myapp",
      "gofastr dev",
      "gofastr build"
    ]
  }
}
```

---

## Error Cases

| Error | Message | Suggestion |
|-------|---------|------------|
| Directory already exists | `Directory '<name>' already exists.` | `Use a different name, or cd into the existing directory and run 'gofastr init .'` |
| Invalid name (spaces, uppercase) | `Project name '<name>' is invalid.` | `Use lowercase letters, digits, and hyphens. Example: my-blog-app` |
| `go` not found | `Go is not installed or not in PATH.` | `Install Go from https://go.dev/dl/` |
| `git` not found | `Git is not installed.` | `Install git or skip with --no-git flag.` |
| Network error (go mod tidy) | `Failed to download dependencies.` | `Check your internet connection and Go proxy settings.` |

---

## Acceptance Criteria

- [ ] `gofastr init myapp` creates a complete project directory with all files listed above
- [ ] `gofastr init myapp --defaults` works without any interactive prompts
- [ ] `gofastr init myapp --json` outputs project creation result as JSON
- [ ] `gofastr init myapp --db postgres` sets the database driver in config and `main.go`
- [ ] Project name validation rejects invalid names with a clear message
- [ ] Existing directory detection works and suggests alternatives
- [ ] `go mod init` and `go mod tidy` run successfully (or skip gracefully if Go not found)
- [ ] `git init` runs and creates an initial commit (or skips gracefully with `--no-git`)
- [ ] The generated `main.go` compiles without errors (assuming gofastr module is available)
- [ ] The generated `gofastr.yaml` is valid and can be loaded by the config loader from task 035
- [ ] `.gitignore` includes `.gofastr/` and `*.db`
- [ ] The example entity JSON is valid and could be loaded by the entity system
- [ ] "Next steps" output is printed with correct commands
- [ ] All tests pass: `go test ./...`

---

## Implementation Notes

- Use Go's `embed` package to embed file templates as string constants or use Go's `text/template` for generation.
- The init command should NOT depend on the full GoFastr framework being built — it just scaffolds files. Only the generated `main.go` references gofastr imports.
- Consider adding a `--template` flag in the future for starter templates (blog, e-commerce, SaaS). Not required now but design the code to allow it.
- Keep the interactive prompts simple. If bubbletea is too heavy for a single command, use `fmt.Scanln` with validation. The key is that `--json` and `--defaults` bypass all prompts.
- For testing: create the project in a temp directory, verify all files exist and have correct content.
