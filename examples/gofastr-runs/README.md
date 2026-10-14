# gofastr-runs

A tracker for every gofastr app running on this machine, built as a
per-user OS service with `battery/service`. It reads the [local run
registry](../../framework/docs/content/local-runs.md), shows the runs in
its web UI, and serves the same list to agents over MCP, each run with
the entry that reaches its own MCP server. The service guide is
[`framework/docs/content/service.md`](../../framework/docs/content/service.md).

## What it shows

- `main.go` is the whole service integration: `service.New(Config{ID,
  Name, Port})` and `svc.Run(ctx, os.Args[1:], buildApp)`. The builder
  runs only in the daemon and the dev run; `status`, `logs` and
  `uninstall` never build the App.
- `runs.go` turns `localrun.List()` into the view both surfaces use:
  a kind (`service`, `service dev run`, `dev`, `app`) and an
  `mcpServers` entry: stdio when the run recorded an `mcp_command`
  (a service's `service mcp`), HTTP when it serves `/mcp`, none
  otherwise.
- `app.go` is the UI, composed from the design system with no CSS:
  `/` is a `DataTable` the page re-fetches every 5 s through
  `data-cui-poll`, so a run that starts or stops appears without a
  reload; `/runs/{id}` is one run's details and its MCP config.
- `tools.go` registers two MCP tools with `WithMCPTools`: `runs_list`
  (optional case-insensitive `name` filter) and `run_get` (by `id`).
  It is a directory: an agent reads the entry for a run and connects to
  that run itself. The tracker proxies nothing and controls nothing.

## Run it

```sh
go run ./examples/gofastr-runs service run --open   # dev run, browser signed in
```

Start any other example (`gofastr dev --dir examples/blog`) and it
appears in the list within 5 s; stop it and it goes.

Install it as a service:

```sh
go build -o gofastr-runs ./examples/gofastr-runs
./gofastr-runs service install      # copy, register, start: port 47811
./gofastr-runs service open         # sign the browser in
./gofastr-runs service status
./gofastr-runs service uninstall --purge
```

`install` copies the binary, so the one you built can be deleted
afterwards; `status` prints where the installed copy lives.

## Connect an agent

The "Connect an agent" card on `/` prints the config with the installed
binary's real path. Its shape:

```json
{
  "mcpServers": {
    "gofastr-runs": { "command": "/path/to/gofastr-runs", "args": ["service", "mcp", "--state-dir", "/path/to/state"] }
  }
}
```

The card fills both paths from `Runtime.MCPCommand()`, so copy it from
the run you want the agent to reach (the dev run's card adds
`--dev`). `service mcp` tunnels through the daemon's peer-checked
control socket, so only the OS user the service runs as gets in; the
HTTP `/mcp` stays behind the browser sign-in.

## Tests

`main_test.go` drives both MCP tools and the screens against a temp
registry. `cmd/gofastr`'s `TestE2E_DevLoop_Examples/gofastr-runs` boots
the example under `gofastr dev` and checks the loopback gate answers.
`battery/service` carries the bridge's own tests (`mcpbridge_test.go`).
