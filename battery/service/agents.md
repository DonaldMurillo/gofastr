# battery/service

EXPERIMENTAL OS service host: `<app> service install` copies the binary
to a managed path and registers it as a per-user service (launchd agent
on macOS, `systemctl --user` unit on Linux, logon-triggered Task
Scheduler task on Windows) that serves the app's UI on
`127.0.0.1:<Config.Port>` to the signed-in user's browser.

**Use this when** the prompt mentions: OS service, background service,
daemon, launchd, LaunchAgent, systemd user unit, Task Scheduler, run at
login, "install it like a program", local web UI for a background app.

**Import:** `github.com/DonaldMurillo/gofastr/battery/service`

**Shape:**
```go
svc := service.New(service.Config{ID: "dev.gofastr.hello", Port: 47811})
err := svc.Run(ctx, os.Args[1:], func(rt service.Runtime) (*framework.App, error) {
	opts, err := rt.AppOptions() // data dir + app.db + persisted secret
	if err != nil {
		return nil, err
	}
	app := framework.NewApp(opts...)
	app.Mount(uihost.New(site))
	return app, nil
})
```
Verbs: `service install [--port N]`, `uninstall [--purge]`, `start`,
`stop`, `restart`, `status`, `logs`, `open`, `rotate-secret`, `run`.
No arguments (or `service run`) is the foreground dev run with its own
state dir and port; `--addr <loopback>` is what `gofastr dev` passes.

**Rules that will bite you if ignored:**
- `Run` replaces `app.Start`. The builder runs only in the daemon and
  the dev run; the CLI verbs never open the database.
- The port is fixed. A held port fails the start with an error naming
  it; there is no fallback port. `install --port` overrides per install.
- Every request needs a loopback Host (or a `Config.AllowHosts` entry),
  no cross-site unsafe method, and the signed session cookie. Sign in
  with `<app> service open` (`--dev` for the dev run). Do not add a way
  around the gate, and never log the sign-in token or session value.
- `install` refuses root and copies the binary; rebuild + `install`
  again to upgrade. Per-user scope only today.
- A signed session is the local user (`internal/localuser`), so
  owner-scoped entities work without battery/auth. If battery/auth is
  linked, auth owns identity and the gate installs none.
- The control channel is a Unix socket in the 0700 state dir, checked
  against the daemon's own OS user. Keep the state path short: AF_UNIX
  caps it near 104 bytes.

**Testing:** set `GOFASTR_SERVICE_DATA_DIR` to a short temp path before
`Run` or `AppOptions`. The example `examples/service-hello` shows a
background writer started from `OnStart`.

Full doc: `framework/docs/content/service.md` (`gofastr docs service`).
