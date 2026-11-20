# Background services (`battery/service`)

`battery/service` turns a GoFastr app into a local background daemon with a browser UI, managed by the OS service manager (`launchd` on macOS, `systemd --user` on Linux, Windows Task Scheduler / Service on Windows) — no root required.

## Quickstart

```go
import (
	"os"

	"github.com/DonaldMurillo/gofastr/battery/service"
	"github.com/DonaldMurillo/gofastr/framework"
)

func main() {
	svc := service.New(service.Config{
		ID:   "dev.example.syncd",
		Name: "Sync Daemon",
	})
	app := framework.NewApp(svc.AppOptions()...)
	// Register entities, cron/queue jobs, and uihost screens...
	app.RegisterBattery(svc)

	// Dispatches `syncd service <verb>` or runs the app:
	os.Exit(svc.Main(app, os.Args[1:]))
}
```

Like `battery/desktop`, `battery/service` opens an owner-only state directory (`app.db`, `secret`, `identity` via `framework/localapp`) and listens on loopback behind a local browser-auth gate so other local processes cannot freely call the daemon's UI or API.

## Verbs

`svc.Main` adds a `service` subcommand to your binary:

| Command | Action |
| --- | --- |
| `<app> service install` | Copies the binary to its managed path, registers the user-level OS service, and starts it |
| `<app> service uninstall` | Stops the daemon, removes the OS service definition, and leaves user data intact |
| `<app> service start / stop / restart` | Controls the running daemon |
| `<app> service status` | Prints running state, PID, and listen address |
| `<app> service open` | Opens the authenticated browser UI (exchanges a short-lived local token for an HttpOnly session cookie) |
| `<app> service run` | Runs in the foreground (useful for local development and debugging) |
| `<app> service logs` | Tails the daemon's log file |
| `<app> service mcp` | Bridges stdio MCP to the running daemon with its local credential |

Distribute service binaries with `gofastr release --service` so the generated `install.sh` / `install.ps1` automatically run `<app> service install` (see [Release](release.md)).

## Agents over MCP

Because the daemon's HTTP `/mcp` route sits behind its browser session gate, dialing the loopback URL directly returns `401`. Instead, `battery/service` records `mcp_command` (`<exe> service mcp --state-dir <dir>`) in the local run registry (`framework/localrun`):

- Point Claude Code, Codex, or any stdio MCP host at `<app> service mcp`.
- The bridge reads the daemon's owner-only credential from its state directory and forwards JSON-RPC requests cleanly to the running daemon without weakening HTTP authentication.

## Common mistakes

- **Dialing the daemon's HTTP `/mcp` URL directly from an agent:** returns `401` by design. Use `<app> service mcp` (or read `mcp_command` from `localrun.List()`).
- **Running `service install` with `sudo`:** `battery/service` targets per-user OS service managers (`launchd` user agents, `systemd --user`). Installing as root splits the state directory and permissions.
- **Forgetting `svc.AppOptions()`:** without it the app won't inherit the service's persistent state dir, SQLite DB, and session secret.
- **Expecting `service uninstall` to wipe `app.db`:** uninstall stops and removes the service registration while preserving user data.
