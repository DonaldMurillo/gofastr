# service-hello

The `battery/service` example: an app binary that installs itself as a
per-user OS service, keeps running after the terminal closes, and is
used from the browser. The guide is
[`framework/docs/content/service.md`](../../framework/docs/content/service.md).

## What it shows

- `main.go` is the whole integration: `service.New(Config{ID, Name,
  Port})` and `svc.Run(ctx, os.Args[1:], buildApp)`. The builder runs
  only in the daemon and the dev run; `status`, `logs` and `uninstall`
  never open the database.
- `Runtime.AppOptions()` opens `app.db` in the mode's state directory
  and passes the persisted secret, so sessions survive a restart.
- One entity (`pulses`) with screens from `app.EntityUI`: the list at
  `/` and `/pulses`, the create form, and the record page.
- An `OnStart` goroutine writes a pulse when the service starts and one
  every minute after. Close the terminal, come back later, and the list
  has grown: that is the service running in the background. A crash
  restart shows up as a second "service started" row with a new pid.
- The dev run and the installed copy keep separate data, secrets and
  ports, so developing never touches the installed copy.

## Run it

```sh
go run ./examples/service-hello service run --open   # dev run, browser signed in
```

`go run ./examples/service-hello` with no arguments is the same dev run
without the browser; sign in with
`go run ./examples/service-hello service open --dev`.

Install it as a service:

```sh
go build -o hello ./examples/service-hello
./hello service install      # copy, register, start: port 47811
./hello service open         # sign the browser in
./hello service status
./hello service logs -f
./hello service uninstall --purge
```

`install` copies the binary, so the `hello` you built can be deleted
afterwards. The installed copy answers to `hello service ...` from its
managed path too (`status` prints the data directory; the binary is in
its `bin/` on macOS and Linux).

Under the dev loop:

```sh
gofastr dev --dir examples/service-hello
```

The page `gofastr dev` opens answers 401 until you run
`go run ./examples/service-hello service open --dev`; the session then
survives every rebuild.

## Tests

`cmd/gofastr`'s `TestE2E_DevLoop_Examples/service-hello` boots the
example under `gofastr dev` and checks the loopback gate answers.
`battery/service` carries the battery's own suite, including a
headless-Chrome sign-in test behind the `chromium` build tag.
