# Local run registry (`framework/localrun`)

When a local GoFastr app starts via `App.Start`, it writes a small owner-only JSON record (`0600`) to `$GOFASTR_RUNS_DIR` (default `<os.UserConfigDir()>/gofastr/runs`, `0700`) and removes it on `Shutdown`. Tools and agents call `localrun.List()` to discover running local apps, URLs, and MCP endpoints without scanning ports.

## What registers by default

- Every `gofastr dev` run and every `battery/service` run (marked via `App.SetLocalRunInfo`).
- Any `App.Start` bound to loopback (`127.0.0.1`, `::1`, `localhost`).
- Public/container binds (`:8080`, `0.0.0.0`) and Go test binaries do **not** register unless opted in (`GOFASTR_RUNS=on` / `GOFASTR_RUNS_DIR`). Opt out anywhere with `GOFASTR_RUNS=off`.

## Discovering running apps

```go
recs, err := localrun.List() // newest first, live processes only
for _, r := range recs {
	fmt.Println(r.Name, r.URL, r.MCPURL(), r.MCPCommand)
}
```

Each record includes `id`, `name`, `pid`, `url`, `mcp_path`, `mcp_command`, `service`, `dev`, `dir`, `exe`, and `started`. `localrun.List()` validates file ownership and schema strictly, verifies each PID (and OS process start time via `<id>.start` so recycled PIDs aren't mistaken for live runs), and prunes dead records automatically.

When connecting an MCP client to a listed run, prefer `MCPCommand` when set (for example `battery/service` sets `<exe> service mcp --state-dir <dir>` because its HTTP `/mcp` sits behind browser auth — see [Services](service.md#agents-over-mcp)); otherwise use `MCPURL()`.

## Common mistakes

- **Dialing a `battery/service` daemon's HTTP `/mcp` URL directly:** returns `401`; run its `MCPCommand` instead.
- **Expecting a record from an app serving `App.Router()` via a custom `http.Server`:** only `App.Start` registers a run.
- **Treating a run record as cryptographic identity:** any process running as the same OS user can write to the runs directory; use records for discovery only.
