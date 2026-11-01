# Ship your API as a CLI

`gofastr generate cli` turns your app's HTTP API into a branded terminal
client you distribute to *your* customers: the `stripe`/`gh` experience
for the app you built. The output is a standalone, stdlib-only
`package main` that imports exactly one thing: your generated
`entities/client` package. Customers authenticate with a scoped API
token minted in your app, and every entity gets the full set of CRUD
operations plus batch operations and a live event stream.

```bash
cd myapp           # the directory holding entities/
gofastr generate cli
go build ./cmd/myapp
```

The generator reads the entity set from your project source
(`entities/*.go`, the same recovery machinery `gofastr pack` trusts).
No app spec or YAML is involved. It needs an enclosing `go.mod` to derive
the client import path.

The output is pure stdlib Go, so it builds for every platform Go
targets. Cross-compile releases with `GOOS`/`GOARCH`
(`GOOS=windows GOARCH=amd64 go build ./cmd/myapp`), and the config file
lands in each OS's native config dir (`os.UserConfigDir`). The default
output directory is `cmd/<binary>/`, the standard installable-main
layout: if your module is public, customers can install directly with

```bash
go install your.module/path/cmd/myapp@latest
```

For closed-source apps, distribute the cross-compiled binaries.

## What your customers get

```
myapp login --url https://app.example.com --with-token   # paste a token once
myapp posts list --published true --sort -created_at -o table
myapp posts list --views-gt 100 -q searchterm --include author
myapp posts get 42
myapp posts create --title "Hello" --views 3
myapp posts create --json @post.json
myapp posts patch 42 --published=false     # explicit zeros survive
myapp posts update 42 --json '{"title":"Edited"}'
myapp customers patch 42 --logo ./acme.png    # an Image or File field takes a path
myapp posts batch-create --json @rows.json # atomic, up to 100
myapp posts batch-delete id1 id2 id3
myapp posts watch                          # live SSE feed, one JSON line/event
myapp posts delete 42
```

Help is built in at every level, always exiting 0: bare `myapp` (or
`help`/`--help`) lists every command, a bare entity command
(`myapp posts`) lists that entity's subcommands, and `--help` on any
verb prints its full flag reference. An unknown subcommand prints the
group usage and exits 2. `version` prints the binary name.

Per verb:

- **list**: an equality filter flag per field (`--status active`),
  `--<field>-in` for a comma list (`--status-in active,trialing` sends
  `status_in`; the bare flag is equality only, so a comma there is part
  of the value), range flags (`--views-gt/-gte/-lt/-lte`) on
  numeric/date/timestamp fields, `--<field>-like` on text fields,
  plus `--sort`, `--page`/`--limit`, `--cursor` (keyset; `--cursor ""`
  fetches the first keyset page, since the server switches to keyset
  mode when the key is present), `--include`,
  `--fields`, and `--param k=v` (repeatable) as the escape hatch for
  anything else. `-q` appears only when the entity declares
  `SearchFields`; `--trashed` only with `SoftDelete`. `-o table`
  renders an aligned table instead of JSON. Table cells, the cursor
  line and API error lines drop control bytes (C0 but tab, DEL and
  C1), so a stored `\x1b[2J` cannot clear the reader's screen. JSON
  output keeps numbers exact: a value past 2^53 in a `--json` body, a
  json-field flag or a response prints with every digit.
- **create / update / patch**: per-field flags OR `--json`
  (inline, `@file`, or `-` for stdin); the two are mutually exclusive.
  Field flags are presence-faithful: only explicitly-set flags enter
  the body (via `flag.FlagSet.Visit`), so `--published=false` really
  sends `false`. The payload is built as a raw map, never squeezed
  through a struct with `omitempty`.
  An Image or File field gets a file flag that takes a path, or `-`
  for stdin (one flag per run can read stdin). A set file flag sends
  the write as multipart/form-data, every part named by its column. A
  file over the server's 32 MiB per-file limit, or a request over its
  64 MiB body limit, fails before anything is sent, and so does a
  JSON field flag set to `null` beside a file. `--json` sends
  JSON, which can carry the stored key of a file already uploaded but
  never a new file.
- **batch-create / batch-update**: `--json` with an array, sent
  through the atomic `_batch` routes; the `{committed, results[]}`
  envelope prints verbatim and a rollback exits 1.
- **composite keys**: a record verb on a composite-key entity takes one
  positional argument per key column, in key order:
  `myapp enrollments get ann math`, `myapp enrollments patch ann math
  --grade B`. Each value is path-escaped into its own segment.
  `create` takes a flag per key column; `update` and `patch` take
  none, since the path names the record. `batch-delete` takes ids as the server spells them, the values
  path-escaped and joined with `/` (`ann/math`). A key value that
  starts with `-` reads as a flag, so the verb prints its usage.
- **watch**: subscribes to `GET {entity}/_events` and prints one
  `{"event":…,"data":…}` line per server event until interrupted.
  No auto-reconnect: wrap it in a shell loop if you need one.
- **custom endpoints**: one command per endpoint declared in
  `gofastr.settings.yml` ([Custom endpoints](endpoints.md)), named after
  it in kebab case: `myapp orders refund-order <id> --reason late`. Path
  values are positional, in path order; every other input field is a
  flag, required unless the Go field is a pointer. `POST`/`PUT`/`PATCH`
  endpoints also take `--json` in place of the flags.

Connection resolution, in order: the `--url` flag →
`<BINARY>_URL`/`<BINARY>_TOKEN` env vars → the config file written by
`login` (`<user-config-dir>/<binary>/config.json`, written 0600 on
create and on every overwrite). A config file that exists but does
not parse is refused with an error naming the file (delete it or
`login` again) rather than silently treated as "not logged in",
which would send every request unauthenticated. The stored token goes
only to the origin (scheme, host and port) `login` stored it with:
when `--url` or `<BINARY>_URL` points somewhere else, the request goes
without it and stderr says why, so a production token never reaches a
local or unknown server. The host compares ASCII case-insensitively, and
a host with a non-ASCII character has no origin, so the stored token
never goes to it: write an internationalized host in its `xn--` form. A `<BINARY>_TOKEN` you set is your choice and
goes wherever the call does. There is no
`--token` flag: a bearer credential on the command line sits in
ps/procfs state every local process can read. Every verb is
signal-cancellable (Ctrl-C interrupts the request, not just the
process). Exit codes: `0` success, `1` API/transport error
(including a rolled-back batch), `2` usage, `4` authentication
failure (401/403).

## Auth: scoped API tokens

The CLI sends `Authorization: Bearer gfsk_…` on every request. Wire
[`auth.TokenMiddleware`](auth.md#service-accounts--scoped-api-tokens)
alongside your session middleware, mount `auth.NewTokensPlugin` so
logged-in users can mint their own scoped tokens in the app, and add
`auth.RequireAPIScopes("/api")` so those scopes are actually enforced
(a `customers:*` token gets 403 off every other resource). Bearer
requests bypass both CSRF layers by design, so the CLI needs no cookie
or CSRF handling. The customer flow:

1. Log in to your app in a browser.
2. Mint a scoped token (`POST /auth/tokens`, plaintext shown once).
3. `echo "$TOKEN" | myapp login --url https://app.example.com --with-token`.

`--with-token` reads stdin so the token never echoes or lands in shell
history; the interactive prompt warns that input echoes (the stdlib has
no termios). `logout` deletes the stored token. Revoke it in the app
to invalidate it server-side.

## Choosing what to expose

Everything is generated by default. Narrow it declaratively:

```bash
gofastr generate cli --only=posts,comments        # entity allow-list
gofastr generate cli --exclude=audit_logs         # entity deny-list
gofastr generate cli --verbs=list,get             # read-only CLI
gofastr generate cli --verbs='posts=list,get;comments=*'
```

Selection names match the entity name, table, or kebab command form; a
typo fails generation rather than silently generating everything.
Verbs: `list get create update patch delete batch-create batch-update
batch-delete watch`. Excluded entities and verbs render nothing, so there
is no dead code to strip. The generated files do not record the
selection. To have `gofastr generate` and `gofastr build` keep the CLI
current, record it as a `cli` entry in `gofastr.settings.yml` (see
[Keeping it current](#keeping-it-current)); a run with no entry prints
one to paste.

Other flags: `--out=cmd/<binary>` (target directory), `--binary=<name>` (command
name; defaults to the project directory, and drives the env-var
prefix), `--api-prefix=api` (must match your `AppConfig.APIPrefix`:
it's baked into the client's base URL so customers pass a bare server
URL), `--dry-run`, `--json`.

## No entities? Generate from an OpenAPI document

An app with a hand-written API has no entity set to generate from, but
it usually maintains an OpenAPI 3 document. `--from-openapi` generates
the same style of CLI from that artifact instead:

```bash
gofastr generate cli --from-openapi openapi.json --binary=barc
gofastr generate cli --from-openapi https://barc.example.com/openapi.json
```

One subcommand per operation, named from `operationId` — a missing,
duplicate, or non-identifier id fails generation, no auto-naming.
Operation summaries carrying terminal-control bytes (ESC, CR, BEL, DEL —
the bytes that rewrite terminal output — or their 8-bit C1 spellings,
U+0080–U+009F, which drive the same escapes without an ESC prefix) are
refused too, since the
generated help prints them verbatim; printable summaries, including
multi-line ones, stay quoted data.
Path, query, and header parameters become typed flags (arrays of
strings repeat the flag; typed arrays are rejected rather than left
unvalidated). A query array sends `tags=a&tags=b` by default and with
`explode: true`, and `tags=a,b` with `style: form, explode: false`,
each item escaped. Generation refuses any other array style
(`spaceDelimited`, `pipeDelimited`, `deepObject`, a path or header
parameter that is not `simple`) rather than send what the server
does not expect. A parameter's `description` leads its flag's help text,
folded to one line and refused when it carries terminal-control bytes.
An operation's parameter overrides a path-level one with the same `name`
and `in`, as OpenAPI defines it, so the flag takes the operation's type
and description;
`application/json` object bodies get a flag per top-level scalar
property plus the `--json` raw escape. An OpenAPI 3.1 type list
`[T, "null"]` reads as `T` (an unset flag sends nothing, and `--json`
can send the `null`); a list naming two value types gets no flag.
A body property named exactly
like a path parameter gets no flag of its own: it repeats the record the
path names, as a composite key's columns do in a GoFastr app's PUT body,
and `--json` can still send it. Binary bodies
(`application/octet-stream` / `format: binary`) take `--file` with `-`
for stdin; a body offering several binary types is sent as
`application/octet-stream`, else the first type by name. A body the spec
does not mark `required` may be left out, and the command then sends no
body: `--file` is optional, and the schema's `required` properties bind
only a body built from field flags (`--title x` without a required
`--text` still fails). JSON responses pretty-print with numbers exact,
and `Client.Do` decodes them the same way; anything else streams raw to
stdout so `barc generate-code ... > code.png` works. `servers[0].url`
splits in two: an absolute URL's origin seeds the default server URL,
and its path (or the whole of a relative URL such as `/api/v3`) becomes
the API prefix every request carries. `--url` and `$<BINARY>_URL` then
take a bare origin: with `servers: [{url: /api/v3}]`, `get-pet-by-id
--url http://localhost:8080 --pet-id 7` requests
`http://localhost:8080/api/v3/pet/7`. `--api-prefix` (or the entry's
`api_prefix`) replaces the server path, and `--api-prefix=""` drops it;
`--from-openapi` on the command line takes the `api_prefix` of the
settings entry for the same document. A server URL whose path already
ends in the prefix is used as it is, so a `login` stored with
`https://pets.example/api/v3` still requests `/api/v3/pet/7`.
A templated server URL (`{host}`) seeds neither. An `http: bearer` or
header `apiKey` security scheme wires into the same env/`login`
machinery. The token goes under the scheme the document's top-level
`security` requires (its first alternative naming one supported scheme),
or the first supported scheme by name when `security` names none; an
operation whose own `security` names another scheme sends the token
under that scheme's header. An alternative that requires two schemes at
once is passed over, since the CLI holds one token. The client refuses redirects, since every request carries the
token: a 3xx answers as an error instead of re-sending the
`Authorization` or apiKey header to the host it names.

The generated tree is fully self-contained (stdlib only, with its own
`internal/client`), so it runs from any directory inside a Go module —
no entities package, no framework dependency. `custom.go` and the
`*.gen.go` rebuild behave exactly as below.

Supported subset, by design: local `#/components/...` `$ref`s (`allOf`
of objects shallow-merges; `oneOf`/`anyOf` bodies fall back to
`--json`), JSON or YAML documents. Cookie parameters, external refs,
`$ref` cycles, path/template mismatches, and multipart bodies fail
generation with a precise error instead of guessing. A spec whose
*only* security scheme is unsupported (OAuth2 flows, apiKey outside a
header) fails too; a spec offering bearer alongside OAuth2 generates
against bearer.

## Extending and regenerating

The CLI is derived from your entities. Every run rebuilds the
`*.gen.go` files and removes the ones the source no longer produces (an
entity you deleted or excluded, a verb you deselected). Each file carries
a `//gofastr:generated` marker and Go's `Code generated ... DO NOT EDIT.`
line; a run that finds one edited names it before replacing it.
**`custom.go` is the one file that is yours: it is created when absent
and never overwritten.** It is the extension seam:

- `customCommands()` is merged over the generated command table; an
  entry with a generated name (`"posts list"`) replaces that command,
  a new name adds one. The built-in `login` and `logout` can be
  replaced the same way. Wrap rather than replace by calling the
  generated run function (`runPostsList(...)`) from your own.
- `configureClient(c *client.Client)` runs before every request-bearing
  command. Use it to install a custom `http.Transport`, default headers,
  or retries.
- Endpoints registered by hand in Go (not in `gofastr.settings.yml`) are
  reachable via the client's raw `Do(ctx, method, path, body, out)`
  escape hatch.

`custom.go` imports the client of the mode that wrote it: the entity SDK
(`entities/client`) or, under `--from-openapi`, the CLI's own
`internal/client`. A run in the other mode finds that import and refuses,
since the file would not build beside the new `*.gen.go` files. Re-run
with `--force` and the generator moves the file to `custom.go.bak` and
writes a fresh `custom.go`; copy your `customCommands` entries and
`configureClient` body across, then delete the backup. `--force` never
replaces an existing `custom.go.bak`.

The rest of the tree is laid out the same way: each `<entity>.gen.go`
holds the command table, the per-entity filter and mutation-field tables,
and one-line wrappers (`runPostsList`-style) binding them; `verbs.gen.go`
holds the shared verb bodies those wrappers call; `main.gen.go`,
`config.gen.go`, `auth.gen.go`, and `output.gen.go` hold the dispatch,
connection, and printing code. Field names are data rows in the tables,
never identifiers, so a field rename never changes the generated function
names.

### Keeping it current

A `cli` entry in `gofastr.settings.yml` makes `gofastr generate` and
`gofastr build` rebuild the CLI after an entity changes. The `config`
keys mirror the flags:

```yaml
codegen:
  generators:
    - name: cli
      output: cmd/myapp            # --out
      config:
        binary: myapp              # --binary
        api_prefix: api            # --api-prefix
        only: [posts, comments]    # --only
        exclude: [audit_logs]      # --exclude
        verbs:                     # --verbs
          posts: [list, get]
          comments: ["*"]
```

`only`, `exclude` and `verbs` are YAML lists. A comma string such as
`only: posts,comments` is refused with the list spelling. `verbs` is
either one list for every entity (`verbs: [list, get]`) or a map from
entity to its list, as above; quote `"*"`, which YAML otherwise reads as
an alias. `api_prefix` may be empty (`""` for an app with no API prefix).
Any other value is a non-empty string.

To build the CLI from an OpenAPI document instead, set
`from_openapi: openapi.json` (`--from-openapi`), which excludes the
selection keys. It takes a file in the project: a build that fetched a
URL would depend on the network. Fetch the document into the repo
instead.

A run from the settings file rewrites only the files whose bytes changed
and prints nothing when the CLI is current.

`gofastr generate cli` reads the same entry. Flags win, key by key, and
the entry fills the rest, so `gofastr generate cli --only=posts` keeps
the entry's binary and output. With `--out` or `--binary` it uses the
entry for that directory or binary; with neither, the only entry. Two or
more entries need `--out` or `--binary` to pick one. `--from-openapi`
takes the entry's binary and output but none of its entity selection.
Only the settings file in the directory it runs in counts. A run that no
entry drove prints the entry that reproduces it, ready to paste under
`codegen.generators`.

### Upgrading a CLI generated before v0.93.0

Older CLIs were one-shot: plain `.go` files the generator refused to
overwrite. The first run on such a tree removes the old files and writes
the `*.gen.go` set. An old file you edited stops that run with its name;
move the edit into `custom.go`, then re-run once with `--force` to
remove the old files. `--force` matters only for that one migration.

Fields with `hidden: true` never appear; `read_only` fields appear as
list filters but not as mutation flags. Writable image and file fields
appear only as file flags on create, update and patch. A field
whose name collides with a reserved flag (`sort`, `page`, `json`,
`url`, …) fails generation with the entity and field named: rename the
field or exclude the entity.

## Common mistakes

- **Mismatched `--api-prefix`.** The prefix is baked into the CLI at
  generate time. If your app sets `AppConfig.APIPrefix: "/api"` but you
  generate with `--api-prefix=""` (or vice versa), every command 404s.
  Match them, and regenerate after changing the app's prefix.
- **No token middleware on the server.** The CLI authenticates with
  `gfsk_` bearer tokens; without `auth.TokenMiddleware` mounted those
  requests arrive anonymous and secure-by-default CRUD returns 401
  (CLI exit code 4). Mount it alongside `SessionMiddleware` and give
  users `TokensPlugin` to mint tokens.
- **Minting scopes without enforcing them.** `TokenMiddleware` alone
  authenticates the token's owner everywhere; the scope list is only
  enforced where you gate it. Mount `auth.RequireAPIScopes("/api")`
  (or per-route `RequireScope`) or a `customers:*` token silently
  carries the user's full capability.
- **Editing a `*.gen.go` file.** The next run rebuilds it and the edit
  is gone; the run names the file first. Put every customization in
  `custom.go` (override, wrap, or add commands).
- **Expecting `--published false` to parse.** Bool field flags follow
  stdlib `flag` semantics: bare `--published` sets true, and the
  explicit-value form needs `=`, as in `--published=false`. The space form
  treats `false` as a positional and stops flag parsing.

## Sibling: SDKs

`gofastr generate sdk` is the library twin of this command: a
downloadable Go module and JS/TS client for the same API, hosted by the
app itself behind a live docs site. See [sdk](sdk.md).
