// Package service installs a GoFastr app binary as a per-user OS
// service and serves its UI to the signed-in user's browser on
// loopback.
//
// EXPERIMENTAL. The verbs, the control-channel protocol, and the
// on-disk layout may change without a major version bump. This package
// is marked Experimental in the stability manifest.
//
// One binary, three roles:
//
//   - The CLI. `<app> service install|uninstall|start|stop|restart|
//     status|logs|open|rotate-secret` never builds the App: no database
//     opens, no migration runs.
//   - The daemon. `<app> service daemon` is what the service manager
//     runs: a launchd agent in the gui/<uid> domain on macOS, a
//     `systemctl --user` unit on Linux, a logon-triggered Task Scheduler
//     task on Windows. It builds the App through the host's builder,
//     binds 127.0.0.1:<Config.Port> (or Config.BindAddress), and opens
//     the control channel.
//   - The foreground dev run. `<app>` with no arguments, or
//     `<app> service run`, is the daemon without registration: its own
//     state directory and port, so it never collides with an installed
//     copy. `gofastr dev` runs it as `<app> --addr <loopback addr>`.
//
// Trust model. Any local process and any web page the user visits can
// reach a loopback port, so the HTTP listener requires three things on
// every request: a Host header naming the loopback address itself (DNS
// rebinding), no cross-site unsafe method (CSRF), and a signed session
// cookie. Clients never find the daemon by probing the port. They ask
// the control channel, a Unix socket inside the 0700 state directory
// (AF_UNIX on Windows too) whose every connection is checked against
// the daemon's own OS user through the kernel's peer credentials. The
// control channel mints 60-second single-use sign-in tokens; `open`
// writes one into a 0600 local page that POSTs it to the daemon, so
// the token never appears in argv, browser history, or a Referer.
//
// A signed session is the OS user the service runs as: the gate puts
// a stable local user (internal/localuser, shared with battery/desktop)
// on every request it lets through, so owner-scoped entities work
// without battery/auth. An app that links battery/auth keeps auth's
// identity, and the gate installs none.
//
// Usage:
//
//	svc := service.New(service.Config{ID: "dev.gofastr.hello", Port: 8790})
//	err := svc.Run(context.Background(), os.Args[1:],
//	    func(rt service.Runtime) (*framework.App, error) {
//	        opts, err := rt.AppOptions()
//	        if err != nil {
//	            return nil, err
//	        }
//	        app := framework.NewApp(opts...)
//	        app.Mount(uihost.New(site))
//	        return app, nil
//	    })
//
// See framework/docs/content/service.md for the full guide.
package service
