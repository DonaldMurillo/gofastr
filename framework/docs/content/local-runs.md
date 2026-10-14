# Local run registry

Every gofastr App writes a small record when its listener binds and
removes it when the server stops. Together the records answer "which
gofastr apps are running on this machine, where, and how does an agent
reach each one" without scanning ports. `framework/localrun` reads and
writes them; `examples/gofastr-runs` is a tracker built on it that
lists the runs in the browser and serves them to agents over MCP.

## What a run writes

`App.Start` writes the record once the port is held, the same moment
`OnReady` hooks fire, and removes it as `Start` returns, which happens
as soon as `Shutdown` begins. Only `App.Start` writes one: an app that
serves `App.Router()` from its own `http.Server` has no record.

Each record is one JSON file in the registry directory:

| Field | Meaning |
|-------|---------|
| `id` | `<pid>-<8 hex>`, also the file name. |
| `name` | `AppConfig.Name`, else the executable's base name. |
| `pid` | The process id. |
| `url` | The base URL a local client dials. A wildcard bind (`:8080`, `0.0.0.0`) is recorded as `http://127.0.0.1:<port>`. |
| `mcp_path` | `/mcp` when the app serves MCP over HTTP (`WithMCP`, the dev loop's auto-mount, or a hand-mounted route); absent otherwise. |
| `mcp_command` | An argv that speaks stdio MCP to the run. `battery/service` sets `<exe> service mcp`, because its HTTP `/mcp` sits behind the browser sign-in. |
| `service` | The `battery/service` `Config.ID`, for a service daemon or its dev run. |
| `dev` | Set under `gofastr dev` (`GOFASTR_DEV`) and for a service battery's foreground run. |
| `dir`, `exe` | The working directory and executable path. |
| `started` | When the record was written (UTC). |

The directory is `$GOFASTR_RUNS_DIR` when set, else
`<os.UserConfigDir()>/gofastr/runs`:

| OS | Default directory |
|----|-------------------|
| macOS | `~/Library/Application Support/gofastr/runs` |
| Linux | `$XDG_CONFIG_HOME/gofastr/runs`, else `~/.config/gofastr/runs` |
| Windows | `%AppData%\gofastr\runs` |

The directory is created owner-only (0700, or an owner-only ACL on
Windows) and each record is written 0600 through a temp file and a
rename, so a reader never sees half a record. A record holds no secret:
a name, a URL, a pid and two paths.

## Reading the registry

```go
recs, err := localrun.List() // newest first
for _, r := range recs {
	fmt.Println(r.Name, r.URL, r.MCPURL(), r.MCPCommand)
}
```

`List` returns live runs only. A run that died without shutting down
(`kill -9`, a crash, a power cut) leaves its file behind; `List` checks
each record's pid and deletes the file of one that has exited. It skips
anything that is not a record: a file whose name is not `<pid>-<hex>.json`,
a body over 16 KiB, a duplicate or unknown key, an id that disagrees
with its file name or pid, and a URL that is not plain `http(s)`.

To reach a run's MCP server, prefer `MCPCommand` when it is set: it
carries its own credential. Otherwise `MCPURL()` is the HTTP endpoint;
under `gofastr dev` it answers on loopback with no credential, and
elsewhere the app's own auth applies.

## What a host adds

`App.SetLocalRunInfo(framework.LocalRunInfo{Service, MCPCommand, Dev})`
sets the fields the App cannot know itself. Call it before `Start`.
`battery/service` calls it for you.

## Opting out

`GOFASTR_RUNS=off` (also `0`, `false`, `no`) keeps a process out of the
registry; listing still works. A failure to write is logged as a
warning naming that opt-out, and the app serves anyway.

Test binaries never register unless `GOFASTR_RUNS_DIR` is set, so a
test that starts an App does not write into the real registry. Neither
does an app a test builds and runs as a child process: a test binary
that links `framework/localrun` marks its environment at init, and the
mark passes down to every process it starts. A test of the registry
itself points `GOFASTR_RUNS_DIR` at `t.TempDir()`, which its children
inherit too.

## Common mistakes

- **Treating a record as proof the port is this app.** Any process of
  the same OS user can write a file into the directory, and a pid freed
  by a crash can be reused by an unrelated process, which keeps the old
  record listed until that process exits. Use the record to find a
  run, then talk to it.
- **Expecting a record from an embedded router.** Only `App.Start`
  writes one. An app mounted inside another server, or a test serving
  `App.Router()` through `httptest`, is not in the registry.
