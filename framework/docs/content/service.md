# OS service host (experimental)

`battery/service` turns a GoFastr app binary into a per-user OS service:
`<app> service install` copies the binary to a managed location,
registers it with the OS service manager, and starts it. From then on
the app runs in the background, starts at login, restarts after a
crash, and serves its UI to the user's browser on a fixed loopback
port. The same binary is the CLI that installs, signs in, and
uninstalls it.

The package is **Experimental** in the stability manifest. The verbs,
the control-channel protocol and the on-disk layout can change without
a major version bump.

| OS | Service manager | Definition |
|----|-----------------|------------|
| macOS | launchd, `gui/<uid>` domain | `~/Library/LaunchAgents/<ID>.plist` |
| Linux | `systemctl --user` | `$XDG_CONFIG_HOME/systemd/user/<ID>.service` (default `~/.config/...`) |
| Windows | Task Scheduler, logon trigger, least privilege | task `\<ID>` |

Every install is per-user. There is no system-wide (root or
LocalSystem) mode yet, and `install` run as root refuses rather than
registering the service in root's account.

## The shape of a `main.go`

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/DonaldMurillo/gofastr/battery/service"
	"github.com/DonaldMurillo/gofastr/framework"
)

func main() {
	svc := service.New(service.Config{ID: "dev.gofastr.hello", Name: "hello", Port: 47811})
	if err := svc.Run(context.Background(), os.Args[1:], buildApp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// buildApp runs in the daemon and the dev run, never for the other verbs.
func buildApp(rt service.Runtime) (*framework.App, error) {
	opts, err := rt.AppOptions()
	if err != nil {
		return nil, err
	}
	app := framework.NewApp(opts...)
	// entities, screens, app.Mount(uihost.New(site)) ...
	return app, nil
}
```

`Run` registers the battery on the App the builder returns, so the
builder does not need to. `examples/service-hello` is the complete
version: an entity, entity screens, and a background goroutine that
writes a row every minute so there is something to see after closing
the terminal.

An app with subcommands of its own checks for them before calling
`Run`. `Run` treats any first argument other than `service` or
`--addr` as a usage error.

## One binary, three roles

- **The CLI.** `<app> service <verb>` never calls the builder: no
  database opens and no migration runs, so `status` on a broken
  install still answers.
- **The daemon.** `<app> service daemon --port N` is what the service
  manager runs. It builds the App, binds `127.0.0.1:N`, and opens the
  control channel. Nobody types this verb.
- **The dev run.** `<app>` with no arguments, or `<app> service run`,
  is the daemon in the foreground with its own state directory and its
  own port, so it never shares a database, secret or port with an
  installed copy.

## Verbs

| Verb | What it does |
|------|--------------|
| `install [--port N]` | Copies the running binary to the managed path, writes the service definition, starts it, and waits up to 15 s for the daemon to answer. Reinstalling stops the old copy first. |
| `uninstall [--purge]` | Stops and unregisters, deletes the installed binary. Data stays unless `--purge`. |
| `start` / `stop` / `restart` | Through the service manager. `stop` is a clean exit, which no backend restarts. |
| `status` | Installed or not, running or not, pid, address, version, data directory. Asked of the daemon, never parsed from manager output. |
| `logs [-n N] [-f]` | The service log. journald on Linux; `logs/service.log` in the state directory on macOS and Windows. Control bytes are stripped before printing. |
| `open [--dev] [--print]` | Signs the browser in (see below). `--dev` targets the dev run. |
| `run [--port N] [--open]` | The foreground dev run. `--open` signs the browser in once it is up. |
| `rotate-secret [--dev]` | Replaces the secret, which signs every browser out, and restarts a running installed daemon so it applies. With `--dev` it prints that the dev run needs a restart. |

## Ports

`Config.Port` is the installed daemon's port. It is fixed so a
bookmark survives restarts; `install --port N` overrides it for one
installation and the override is written into the service definition.
If another program holds the port, the daemon exits with an error that
names it, the service manager shows a failed start, and `install`
reports that it did not come up. There is no fallback port: a service
that moved would break every bookmark without saying so.

`Config.DevPort` is the dev run's port. Zero picks a free one, and
worktree isolation remaps it like any App's. The installed daemon
turns isolation off: its port is part of its contract.

## Where state lives

| Path | Contents |
|------|----------|
| `<UserConfigDir>/<ID>/` | Installed copy's state, 0700 (an owner-only DACL on Windows) |
| `<UserConfigDir>/<ID>.dev/` | Dev run's state, same layout |
| `.../app.db` | The SQLite database `Runtime.AppOptions` opens |
| `.../secret` | 32 random bytes, base64url, 0600. Signs sessions and is passed to `framework.WithSecret` |
| `.../identity` | The local user id (see "Who the signed-in browser is") |
| `.../control.sock` | The control channel |
| `.../logs/service.log` | macOS and Windows log, trimmed to one 10 MB generation at start |
| `.../bin/<Name>` | The installed binary (macOS, Linux) |
| `%LOCALAPPDATA%\Programs\<ID>\<Name>.exe` | The installed binary (Windows) |

`UserConfigDir` is `~/Library/Application Support` on macOS,
`$XDG_CONFIG_HOME` (default `~/.config`) on Linux, and `%AppData%` on
Windows. `GOFASTR_SERVICE_DATA_DIR`, an absolute path, replaces it;
tests use it so a suite never touches the real one. The control socket
path must fit the platform's AF_UNIX limit (about 104 bytes), and the
daemon refuses to start with a message naming the variable when it
does not.

`battery/desktop` uses the same base directory, so a desktop app and a
service app given the same `ID` share a state directory. Give them
different IDs.

## Signing in

A loopback port is reachable by every local process and by every web
page the user's browser opens, so the listener checks three things on
every request, the framework's own routes included:

1. **Host.** The `Host` header must be the daemon's own address
   (`127.0.0.1:<port>` or `localhost:<port>`) or a `Config.AllowHosts`
   entry. This stops DNS rebinding; an `Origin` check alone does not.
   A wrong Host gets 403.
2. **Cross-site.** An unsafe method from another site gets 403
   (`handler.IsCrossSiteRequest`).
3. **Session.** A cookie signed with a key derived from the secret.
   Without one the answer is 401 and the body names the command to
   run: `hello service open`.

The gate is installed with [`App.WrapServer`](security.md#the-default-stack),
so it stands in front of everything the listener serves: every route,
the SSE bus, the UI host's pages, and the interactive setup wizard.

`open` gets a session onto the browser without a password:

1. It asks the daemon, over the control channel, for a sign-in token:
   32 random bytes, single use, valid for 60 seconds.
2. It writes a 0600 HTML page into the state directory's `login/`
   folder holding a form that POSTs the token to
   `/__gofastr/service/login`, and opens that page.
3. The daemon burns the token, sets an `HttpOnly`, `SameSite=Strict`
   cookie named for the port, and answers with a page that refreshes
   to `/`. A redirect would arrive without the cookie: the chain began
   on a `file://` page, so it is cross-site, and a Strict cookie is not
   sent on it.

The token never appears in argv, shell history, browser history, or a
Referer. `open` deletes login pages older than five minutes each time
it runs.

Sessions last `Config.SessionTTL` (default seven days). The cookie name
carries the port, so the installed daemon and a dev run of the same app
keep separate sessions in one browser.

### Headless machines

On a machine with no browser, reached over `ssh -L`, set
`Config.InsecureTokenInURL` and run `open --print`. It prints a one-shot
`http://127.0.0.1:<port>/__gofastr/service/login?t=...` URL. The token
is still single use and expires in 60 seconds, but it passes through
the terminal and the browser's history, which is why it is off by
default and logged as a warning at every start.

## The control channel

Clients never find the daemon by probing its port. `status`, `open`,
`stop` and the others connect to `control.sock` in the state directory:
a Unix domain socket on every OS, AF_UNIX included on Windows. The
directory is owner-only, and every connection is also checked against
the daemon's own OS user through the kernel's peer credentials
(`LOCAL_PEERCRED` on macOS, `SO_PEERCRED` on Linux, the peer process's
token on Windows). Requests are one strict JSON object each: unknown,
duplicate and case-folded keys are refused, and the size is bounded.

The socket is also the single-instance lock. A second daemon or dev
run of the same mode finds a live socket and refuses before it builds
the App, so it never opens a database the first one is using. A stale
socket left by a crash is detected and replaced.

## Who the signed-in browser is

A signed session is the OS user the service runs as. The gate puts a
local user on each request it lets through: a stable id minted once
into `identity` in the state directory, with the `admin` role.
Owner-scoped entities (`EntityConfig.Scope.OwnerField`) and anything
that calls `handler.GetUser` see that user, so an app with no auth
battery still has an owner for its rows. `battery/desktop` mints its
window's identity the same way.

An app that also links `battery/auth` keeps auth's identity end to
end. The gate still gates, but installs no local user: auth's own
sign-in decides who a request is, and an anonymous request stays
anonymous.

A corrupt `identity` file stops the daemon with an error instead of
minting a new id, because a new id would orphan every row the old one
owns. Restore the file, or delete it to start a new identity.

## Development

- `go run .` (or `<app> service run`) is the foreground dev run.
  `service open --dev` signs a browser in to it; `service run --open`
  does both.
- `gofastr dev` runs the app as `<app> --addr <addr>`, which is the dev
  run on that address's port, gate included. The address must be
  loopback: `localhost`, `127.0.0.1`, or an empty host. Sign in once
  with `go run . service open --dev`; the session survives every
  rebuild, because the dev run's secret and port do not change.
- `Runtime.Dev` tells the builder which mode it is in, for anything
  that should differ (the example labels its rows "dev run").

## Signals and shutdown

The battery owns signal handling (it sets
`AppConfig.DisableSignalHandling`). SIGINT, SIGTERM, a control-channel
`stop`, and a cancelled `Run` context all drain the App and exit 0.
On Linux the unit is `Type=notify`: the daemon sends `READY=1` once it
is serving and `STOPPING=1` before the drain, so `systemctl --user
start` reports a port collision as a failed start.

## Platform notes

- **macOS.** `install` runs `launchctl bootstrap` and `enable`;
  `KeepAlive` restarts a crash but leaves a clean exit down. On macOS
  13 and later the system shows a "Background Items Added" notification
  and lists the agent under System Settings → General → Login Items &
  Extensions, attributed to the binary ("unidentified developer" when
  unsigned), where the user can switch it off. To ship a signed build, sign the binary (or build it
  with `gofastr desktop build --sign/--notarize`) before `install`
  copies it.
- **Linux.** A user unit runs only while the user has a session.
  `install` prints the `loginctl enable-linger` command to keep it
  running after logout, and does not run it: that changes what starts
  at boot for the whole account. Without systemd as init (a container,
  WSL without systemd) `install` refuses and prints the `service daemon`
  command to run under your own supervisor.
- **Windows.** A Task Scheduler task, not a Windows service: a service
  runs in session 0 under another account and needs an administrator
  to install. Task Scheduler sends no stop signal a console-less process
  can catch, so `stop` asks the daemon to drain over the control
  channel and falls back to `schtasks /End`. A console binary flashes
  a window at logon before the daemon hides it; build with
  `-ldflags=-H=windowsgui` to avoid that. The task XML, its argument
  quoting and the uninstall argv are unit-tested on every OS, but the
  Windows backend has not yet been run on a real Windows machine.

## Limits

- **Cookies are shared across ports.** Browsers do not isolate cookies
  by port, so the browser also sends this cookie to any other web
  server on `127.0.0.1` the user visits. A hostile local server, run by
  another account on the same machine, could collect it and replay it
  until the session expires. `rotate-secret` ends every session.
- **Windows port exclusivity.** Go does not set `SO_EXCLUSIVEADDRUSE`,
  so on Windows a process running as the same user can bind the same
  port with `SO_REUSEADDR` and receive some connections.
- **No LAN mode.** The daemon binds `127.0.0.1` only. `AllowHosts`
  admits a reverse proxy's Host name; it does not change the bind.

## Common mistakes

- **Building the App in `main` instead of the builder.** The CLI verbs
  never call the builder, which is what keeps `status` and `uninstall`
  working when the database is broken. Code outside the builder runs
  for every verb.
- **Registering a second `service.Battery`.** `Run` registers its own.
  Registering a different instance on the App is an error at start.
- **Looking for the port by scanning.** Ask `<app> service status`, or
  in code, read the address the control channel reports. A port that
  answers is not proof it is this app.
- **Same `ID` for a desktop app and a service app.** They share a state
  directory; give them different IDs.
