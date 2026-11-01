# Local run registry

Every gofastr App writes a small record when its listener binds and
removes it when the server stops. Together the records answer "which
gofastr apps are running on this machine, where, and how does an agent
reach each one" without scanning ports. `framework/localrun` reads and
writes them; `examples/gofastr-runs` is a tracker built on it that
lists the runs in the browser and serves them to agents over MCP.

## What a run writes

`App.Start` writes the record (for a run that registers; see [Which
runs register](#which-runs-register)) once the port is held, the same moment
`OnReady` hooks fire, and removes it as `Start` returns, which happens
as soon as `Shutdown` begins. Only `App.Start` writes one: an app that
serves `App.Router()` from its own `http.Server` has no record.

Each record is one JSON file in the registry directory:

| Field | Meaning |
|-------|---------|
| `version` | The record layout, `1`. A reader skips a layout it does not know. |
| `id` | `<pid>-<8 hex>`, also the file name. |
| `name` | `AppConfig.Name`, else the executable's base name. |
| `pid` | The process id. |
| `url` | The base URL a local client dials. A wildcard bind (`:8080`, `0.0.0.0`) is recorded as `http://127.0.0.1:<port>`. |
| `mcp_path` | `/mcp` when the app serves MCP over HTTP (`WithMCP`, the dev loop's auto-mount, or a hand-mounted route); absent otherwise. |
| `mcp_command` | An argv that speaks stdio MCP to the run. `battery/service` sets `<exe> service mcp --state-dir <dir>`, because its HTTP `/mcp` sits behind the browser sign-in. |
| `service` | The `battery/service` `Config.ID`, for a service daemon or its dev run. |
| `dev` | Set under `gofastr dev` (`GOFASTR_DEV`) and for a service battery's foreground run. |
| `dir`, `exe` | The working directory and executable path. |
| `started` | When the record was written (UTC). |

Beside the record sits `<id>.start`, the OS's start value for `pid` as a
decimal number (clock ticks after boot on Linux, the start time in
microseconds on macOS, the creation FILETIME on Windows), only ever
compared with a later reading of the same pid. It is written first, and
absent when the OS would not say. It lives outside the JSON so the
record stays the layout readers already know: a reader decodes records
strictly, and a new field would make one built against an older
framework drop every record. `Record.ProcStart` carries it in Go.

The directory is `$GOFASTR_RUNS_DIR` when set, else
`<os.UserConfigDir()>/gofastr/runs`:

| OS | Default directory |
|----|-------------------|
| macOS | `~/Library/Application Support/gofastr/runs` |
| Linux | `$XDG_CONFIG_HOME/gofastr/runs`, else `~/.config/gofastr/runs` |
| Windows | `%AppData%\gofastr\runs` |

The directory is created owner-only (0700, or an owner-only ACL on
Windows) and each record is written 0600 through a temp file and a
rename, so a reader never sees half a record. Registering and listing
both refuse a directory that is a symlink, is not a directory, or
belongs to another user (on Windows: neither the user's SID nor the
token's default owner), and one swapped in between that check and the
open. One of the user's own that group or others can reach is
tightened back to 0700. A record holds no secret: a name, a
URL, a pid, its start value and two paths.

## Reading the registry

```go
recs, err := localrun.List() // newest first
for _, r := range recs {
	fmt.Println(r.Name, r.URL, r.MCPURL(), r.MCPCommand)
}
```

`List` returns live runs only. A run that died without shutting down
(`kill -9`, a crash, a power cut) leaves its file behind; `List` checks
each record's pid and deletes the file of one that has exited. Once a
run is gone the OS may hand its pid to a newer process, so `List` also
compares the `.start` value with the pid's start value now and deletes
the record when they differ. A record without a `.start` file, or a pid
whose start value the OS will not report, falls back to the pid check.
A `.start` file whose record is gone (a reader that predates it deleted
the record) is removed once its pid has exited. It skips
anything that is not a record: a FIFO or other non-regular file, a file
another user owns (one planted while the directory was loose), a file
whose name is not `<pid>-<hex>.json`, a body over 16 KiB, a duplicate
or unknown key, a `version` other than 1, an id that disagrees with its
file name or pid, a URL that is not plain `http(s)`, and an `mcp_path`
that does not start with `/` (so `MCPURL()` cannot name another host).

To reach a run's MCP server, prefer `MCPCommand` when it is set: it
carries its own credential. Otherwise `MCPURL()` is the HTTP endpoint;
under `gofastr dev` it answers on loopback with no credential, and
elsewhere the app's own auth applies.

## What a host adds

`App.SetLocalRunInfo(framework.LocalRunInfo{Service, MCPCommand, Dev})`
sets the fields the App cannot know itself. Call it before `Start`.
`battery/service` calls it for you.

## Which runs register

The registry is for tools on the same machine, so only a local run
writes a record by default:

- a run under `gofastr dev`, or one a host marked with
  `SetLocalRunInfo` (`battery/service` does), wherever it binds;
- any other app whose listener is on a loopback address
  (`127.0.0.1`, `::1`, `localhost:<port>`).

A server bound to every interface (`:8080`, `0.0.0.0`) or to a LAN
address is a deployment, such as a container or a VM. It writes
nothing, because no local tool reads its record and its home directory
is often missing or read-only. `GOFASTR_RUNS=on` (also `1`, `true`,
`yes`) registers any run. `GOFASTR_RUNS_DIR` only says where the
registry is.

## Opting out

`GOFASTR_RUNS=off` (also `0`, `false`, `no`) keeps a process out of the
registry; listing still works. A failure to write is logged and the app
serves anyway: at debug level for a run that registered on its own, as
a warning naming the opt-out when `GOFASTR_RUNS=on` asked for it. In Go
the variable's name is `localrun.EnvRuns` (`localrun.EnvOff` is the
same constant under its older name).

Test binaries never register unless `GOFASTR_RUNS_DIR` is set, so a
test that starts an App does not write into the real registry. Neither
does an app a test builds and runs as a child process: a test binary
that links `framework/localrun` marks its environment at init, and the
mark passes down to every process it starts. A test package that
builds and runs an app but imports nothing from gofastr links no
`localrun`, so it adds
`import _ "github.com/DonaldMurillo/gofastr/framework/localrun"` to one
test file, or sets `GOFASTR_RUNS=off` on the child. A test of the
registry itself points `GOFASTR_RUNS_DIR` at `t.TempDir()`, which its
children inherit too.

## Common mistakes

- **Treating a record as proof the port is this app.** Any process of
  the same OS user can write a file into the directory, and a pid freed
  by a crash can be reused by an unrelated process, which keeps the old
  record listed until that process exits. A run that exited but was
  never reaped by its parent (a zombie) still answers the pid check, so
  its record stays listed until the parent reaps it. Use the record to
  find a run, then talk to it.
- **Expecting a record from an embedded router.** Only `App.Start`
  writes one. An app mounted inside another server, or a test serving
  `App.Router()` through `httptest`, is not in the registry.
- **Dialing the URL of a service daemon's `/mcp` directly.** The
  service gate answers 401 to anything without the browser's session
  cookie. Use its `mcp_command`; see
  [the service guide](service.md#agents-over-mcp).
