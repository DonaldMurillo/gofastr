# Task 039: CLI Dev Command

**Phase:** 4 — CLI & DX  
**Depends on:** 038 (Build)  
**Status:** not started

---

## Goal

Implement `gofastr dev` — the hot-reload development server that watches for file changes, regenerates code, rebuilds the binary, and restarts the server automatically. This is the primary command developers use during development.

---

## Context

From the draft:

> `gofastr dev` → hot-reload: watch → generate → build → restart

From the proposal:

> **Codegen approach** ✅ Custom build step (`gofastr build`) with `gofastr dev` for hot-reload file watching

The dev command wraps the build pipeline (generate + compile) with a file watcher and a managed server process. When any source file changes, it automatically rebuilds and restarts the server.

---

## Requirements

### 1. Command Definition

```
gofastr dev [flags]
```

### 2. Flags

| Flag | Type | Default | Purpose |
|------|------|---------|---------|
| `--port` / `-p` | int | `3000` | Dev server port. |
| `--host` | string | `"localhost"` | Dev server host. |
| `--open` / `-o` | bool | `true` | Auto-open browser on first successful start. |
| `--no-open` | bool | false | Explicitly disable browser opening. |
| `--watch` | stringSlice | auto | Additional watch paths. Default: `[".", "entities/", "templates/", "static/"]`. |
| `--ignore` | stringSlice | auto | Additional ignore patterns. Default: `[".gofastr/", ".git/", "vendor/", "node_modules/", "bin/"]`. |
| `--debounce` | duration | `200ms` | Debounce window for file change events. |
| `--no-generate` | bool | false | Skip code generation on changes (just rebuild + restart). |
| `--verbose` / `-v` | bool | false | Print file change events and detailed rebuild info. |

### 3. Dev Server Lifecycle

```
gofastr dev
  │
  ├── Step 1: Initial Build
  │   ├── Generate code (gofastr generate)
  │   ├── Build binary (go build)
  │   └── If build fails, print errors and wait for file changes
  │
  ├── Step 2: Start Server
  │   ├── Start binary as subprocess: ./bin/myapp --port 3000
  │   ├── Wait for health check (GET / → 200)
  │   ├── If --open: open http://localhost:3000 in browser
  │   └── Proxy server stdout/stderr to terminal (prefixed with [app])
  │
  ├── Step 3: Watch Files
  │   ├── Watch configured paths
  │   ├── Ignore configured patterns
  │   └── On change → Step 4
  │
  ├── Step 4: Rebuild & Restart
  │   ├── Debounce changes (wait --debounce duration)
  │   ├── Print "Rebuilding..." message
  │   ├── Regenerate (if enabled)
  │   ├── Rebuild binary
  │   ├── If build succeeds:
  │   │   ├── Gracefully stop server (SIGTERM, wait 5s, then SIGKILL)
  │   │   ├── Drain active connections (wait up to 10s)
  │   │   ├── Start new server process
  │   │   └── Print "Server restarted (0.8s)"
  │   └── If build fails:
  │       ├── Keep old server running (if it was running)
  │       ├── Print build errors to terminal
  │       └── If browser error overlay enabled, push error to overlay
  │
  ├── Step 5: Graceful Shutdown (Ctrl+C)
  │   ├── Stop file watcher
  │   ├── SIGTERM to server process
  │   ├── Wait for graceful shutdown (up to 10s)
  │   └── Print "Dev server stopped"
  │
  └── (Loop: Step 3 → Step 4 on each change)
```

### 4. File Watching

Use `fsnotify/fsnotify` or [coder/fsnotify](https://github.com/coder/fsnotify).

#### Default watch paths

```go
defaultWatchPaths := []string{
    ".",           // all .go files in root
    "entities/",   // entity JSON/Go files
    "templates/",  // HTML templates
    "static/",     // static assets
    "migrations/", // migration files
}
```

#### File type filtering

Only trigger rebuild for these file types:
- `.go` files
- `.json` files (in entities/)
- `.yaml` / `.yml` files (config)
- `.html` / `.tmpl` files (templates)
- Static assets trigger copy/static refresh only (no rebuild if server can serve them directly)

#### Default ignore patterns

```go
defaultIgnorePatterns := []string{
    ".gofastr/",
    ".git/",
    "vendor/",
    "node_modules/",
    "bin/",
    "*.db",
    "*.log",
}
```

### 5. Debounced Rebuild

- On first file change event, start a timer of `--debounce` duration (default 200ms).
- If more changes arrive during the debounce window, reset the timer.
- When timer fires, execute the rebuild.
- This prevents rapid successive rebuilds when saving multiple files.

### 6. Graceful Server Restart

```go
func restartServer(old *os.Process, newBinary string, port int) error {
    if old != nil {
        // Send SIGTERM
        old.Signal(syscall.SIGTERM)
        
        // Wait up to 5s for graceful shutdown
        done := make(chan error, 1)
        go func() { _, err := old.Wait(); done <- err }()
        
        select {
        case <-done:
            // Server exited cleanly
        case <-time.After(5 * time.Second):
            // Force kill
            old.Signal(syscall.SIGKILL)
            <-done
        }
    }
    
    // Start new server
    cmd := exec.Command(newBinary, "--port", strconv.Itoa(port))
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Start()
}
```

### 7. Browser Error Overlay

When a build fails and the old server is running, inject an error page that the browser can display:

- The dev server should serve a special `/__gofastr_error` endpoint that returns the last build error as JSON.
- When a build fails, the terminal error should include: "Build errors are available at http://localhost:3000/__gofastr_error".
- Optionally: inject a JavaScript snippet into HTML responses that polls `/__gofastr_error` and displays an overlay.

#### Error overlay JSON format

```json
{
  "status": "error",
  "error": "go build failed",
  "output": ".gofastr/entities/posts_routes.go:42: undefined: PostHandler",
  "file": "posts_routes.go",
  "line": 42,
  "suggestions": ["Run 'gofastr build --clean' to regenerate"]
}
```

### 8. Auto-Open Browser

- On first successful server start, open `http://localhost:<port>` in the default browser.
- Use `os/exec.Command("open", url)` on macOS, `xdg-open` on Linux, `start` on Windows.
- Only open once per `gofastr dev` session (not on every restart).
- Disable with `--no-open` or config `dev.open_browser: false`.

### 9. Terminal Output

#### Human mode

```
$ gofastr dev

  GoFastr dev server v0.1.0

  ✓ Generated 8 files (0.2s)
  ✓ Built binary (0.9s)
  ✓ Server started at http://localhost:3000

  Watching for changes...
  (Press Ctrl+C to stop)

  [change] entities/posts.json
  Rebuilding...
  ✓ Regenerated 8 files (0.1s)
  ✓ Rebuilt binary (0.7s)
  ✓ Server restarted (0.8s)

  [change] main.go
  Rebuilding...
  ✗ Build failed:
    main.go:15: undefined: gofastr.New
    (Server still running with previous build)
```

#### JSON mode (`--json`)

Each event is a JSON line:

```json
{"event": "start", "data": {"port": 3000, "url": "http://localhost:3000"}}
{"event": "change", "data": {"file": "entities/posts.json", "op": "write"}}
{"event": "rebuild_start", "data": {}}
{"event": "rebuild_success", "data": {"generate_ms": 100, "build_ms": 700, "total_ms": 800}}
{"event": "rebuild_error", "data": {"step": "compile", "output": "main.go:15: undefined: gofastr.New"}}
```

### 10. Signal Handling

| Signal | Action |
|--------|--------|
| `SIGINT` (Ctrl+C) | Graceful shutdown: stop watcher, stop server, exit 0 |
| `SIGTERM` | Same as SIGINT |
| `SIGHUP` | Force rebuild (manual trigger without file change) |

### 11. Configuration

Dev settings from `gofastr.yaml`:

```yaml
dev:
  port: 3000
  host: "localhost"
  open_browser: true
  watch_paths:
    - "."
    - "entities/"
    - "templates/"
    - "static/"
  ignore_paths:
    - ".gofastr/"
    - ".git/"
    - "vendor/"
  debounce_ms: 200
```

CLI flags override config file values.

---

## Acceptance Criteria

- [ ] `gofastr dev` builds the project and starts the server
- [ ] Server is accessible at the configured port (default 3000)
- [ ] Changing a `.go` file triggers a rebuild and server restart
- [ ] Changing an entity JSON file triggers regeneration + rebuild + restart
- [ ] Changing a file in `.gofastr/` does NOT trigger a rebuild
- [ ] Multiple rapid file changes are debounced into a single rebuild
- [ ] Build errors are printed to terminal without killing the running server
- [ ] Server restart is graceful: SIGTERM → wait → SIGKILL
- [ ] `Ctrl+C` cleanly stops the watcher and server
- [ ] `--open` opens the browser on first successful start
- [ ] `--no-open` prevents browser from opening
- [ ] `--port 8080` uses port 8080
- [ ] `--verbose` prints each file change event
- [ ] `--json` outputs structured JSON events (one per line)
- [ ] Static file changes don't trigger a full rebuild (if server can serve them directly)
- [ ] Build errors are available at `/__gofastr_error` endpoint
- [ ] Server health check confirms restart before reporting success
- [ ] All tests pass: `go test ./...`

---

## Implementation Notes

- Use `fsnotify` for file watching. It's the standard Go library for this.
- The dev command manages a subprocess (the built binary). Use `os/exec.Cmd` with `SysProcAttr` set to create a new process group for clean signal handling.
- On macOS, `lsof -i :<port>` can check if the port is in use before starting. Or try binding and handle the error.
- The debounce timer should be a `time.AfterFunc` that gets reset on each event.
- For the error overlay: consider a lightweight approach — the generated binary includes a dev mode middleware that serves the error endpoint. This avoids needing a separate proxy server.
- Consider using air's approach as reference: https://github.com/cosmtrek/air — but keep it simpler.
- For testing: start `gofastr dev` in a subprocess, write a file, verify restart happens, send SIGINT, verify clean exit.
