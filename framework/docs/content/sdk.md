# Ship your API as SDKs

`gofastr generate sdk` turns your app's HTTP API into downloadable client
SDKs, a standalone, stdlib-only **Go module** and a zero-dependency
**JS/TS client** (one handrolled ESM `client.js` plus `client.d.ts`),
and `framework/sdkdocs` serves them from the app itself behind a public
docs site with a live per-entity API reference.

```bash
cd your-app          # the directory holding entities/
gofastr generate sdk
```

Like `generate cli`, the entity set comes from your project source
(`entities/*.go`), not an app spec. SDK output is **generator-owned and
regenerates in place**: when the docs site warns that downloads drifted
from the live schema, the fix is re-running the command.

## What gets generated

```
gen/sdk/
  go/          client.go + go.mod + README.md   (standalone module)
  js/          client.js + client.d.ts + README.md
  dist/        sdk-go.zip  client.js  client.d.ts  manifest.json
```

`dist/` is what the app serves: the Go SDK zipped (entries under
`<app>-sdk/`), the two JS files verbatim, and a `manifest.json` recording
sizes, sha256 hashes, the entity set, and a schema hash (see Drift
detection). Archives are deterministic: regenerating an unchanged schema
produces byte-identical output.

Hidden fields never appear in any generated file: the SDKs are public
downloads, and a hidden column's name is schema the API deliberately
conceals.

### The Go SDK

The same typed client the generated app embeds (`entities/client`),
packaged as its own module (`module local/<app>-sdk` by default,
`--module` to override). Per entity: `List/Get/Create/Update/Patch/Delete`,
the three `_batch` verbs, `Watch` (SSE), one method per state move and per
[custom endpoint](endpoints.md) declared in `gofastr.settings.yml`, plus
the `Do` escape hatch,
bearer-token auth (`c.Token = "gfsk_…"`), presence-aware write structs
(an `Input` whose fields are pointers, except a required field with no
default, so `new(false)` overrides
a `true` default, and an all-pointer `Patch`), and `*APIError` whose body carries the error envelope. Every
response body the client buffers — the JSON decode and the `*APIError`
snapshot — is capped at 1 MiB (`maxBodyBytes`): a bigger body is a
misbehaving endpoint, and the decode fails on truncation instead of
buffering it. `Watch` holds one SSE event's joined `data:` lines to the
same cap and returns an error past it. The built-in `http.Client` refuses redirects (requests
carry the bearer token when `Token` is set, and a 3xx would re-send it
to whatever origin the response names); pass your own `*http.Client` to
keep a redirect policy. Consumers

```bash
curl -LO https://your-app.example.com/docs/api/sdk/go.zip
unzip go.zip
go mod edit -replace local/myapp-sdk=./myapp-sdk
```

An entity with an Image or File field also gets `Create<Entity>WithFiles`,
`Update<Entity>WithFiles` and `Patch<Entity>WithFiles`. Each takes the
usual write struct plus an `<Entity>Files` struct holding a `*FilePart`
per file field, and sends one multipart/form-data request with every
part named by its column. With no file set it sends JSON, as
`Create<Entity>` does. Setting a field in the body and sending a file
for it too is an error, since the server would keep the file and drop
the value; a required file field's empty string is no setting and is
left out. A multipart body cannot carry `null`, so a null value is an
error too: clear a field in a separate JSON patch. A file over the
server's 32 MiB per-file limit, or a body over its 64 MiB limit, fails
before anything is sent. Read-only file fields get no upload slot. `DoMultipart` is
the escape hatch for any other multipart route.

```go
f, _ := os.Open("acme.png")
defer f.Close()
created, err := c.CreateCustomersWithFiles(ctx, client.CustomersInput{Name: "Acme"},
	client.CustomersFiles{Logo: &client.FilePart{Name: "acme.png", Body: f}})
// created.Logo is the stored file's key.
```

A composite-key entity's record methods take one argument per key
column, in key order, and path-escape each into its own segment:
`GetEnrollments(ctx, studentId, courseId)`, and likewise `Update`,
`Patch`, `Delete` and each move. A `BatchUpdate` item carries the key
columns as fields (`EnrollmentsBatchPatch{StudentId: s, CourseId: c,
Grade: new("A")}`), and `BatchDelete` takes ids spelled by
`EnrollmentsID(studentId, courseId)`.

An entity that declares an integer `id` (`{name: id, type: int,
auto_generate: increment}`) gets an `ID int`, and its record methods
and `BatchDelete` take `int` ids, since the server answers that id as
a JSON number. The app's own `entities` package types it the same way:
the model's `ID`, the repo's `Get`/`Update`/`Delete`/`Exists`/
`Transition` and `BatchUpdate`/`BatchDelete`, the `<Entity>ID` column,
a cascade child's `ID` in a patch, and the id `On<Entity>Deleted` passes,
so `repo.Get(ctx, row.ID)` and `TicketsID.Eq(row.ID)` take the id a row
holds as it is. Every other entity keyed on `id` keeps `ID string`.
A cascade child keyed on one other column carries that key in the
parent's patch instead of an `ID`: a `has_many` child declared with
`primary_key: [isbn_code]` nests as `IsbnCode string`, since the server
finds the child to update by its key.

`?include=<field>` replaces a relation field's key with the related
record under the same JSON name. The Go client keeps the field typed as
the key: a record decoded from an include response still holds the key
in `AuthorId`, and the related record's raw JSON sits in
`Included["authorId"]` for the caller to decode. The field is never
serialized (`json:"-"`); an entity that already has an `Included` field
gets `Included_`.

A `json` field is typed `any` in every shape: the server stores any JSON
value there, so a row holding `[1, 2]` reads back as
`[]any{json.Number("1"), json.Number("2")}` and an object as
`map[string]any`. Numbers inside an `any` decode as `json.Number`, so
an integer past 2^53 keeps its digits; call `.Int64()` or `.Float64()`
on one rather than asserting `float64`. Set an optional one with
`new(any([]string{"a"}))`. The `client.d.ts` twin is `unknown`.

### The JS/TS SDK

Two plain files, no npm packaging, no build step; publishing to a
registry is your business, not the generator's (add your own
package.json if you want it on npm). `client.js` is directly importable
from disk **or straight from the running app's URL**; `client.d.ts` types
it for TypeScript when the files sit side by side. Its `<Entity>Input`
marks a field optional unless it is required with no default, and
`<Entity>Patch` is a `Partial` of it without the state, owner and
tenant fields an update never sets.

```js
import { Client, ApiError, postsFields } from "./client.js";

const api = new Client({ baseURL: "https://your-app.example.com/api", token });
const page = await api.posts.list({ sort: "-created_at", filters: { [postsFields.views + "_gte"]: 10 } });
await api.posts.patch(id, { views: 0 });   // JS objects are presence-faithful
await api.posts.watch((event, data) => console.log(event), { signal });
```

An `_in` filter takes an array, which goes as one comma-joined value
(`filters: { status_in: ["open", "paid"] }` sends `status_in=open,paid`).
The server splits it on commas, so an element holding a comma throws
before any request is sent. An array on any other filter key throws too,
naming the `_in` key to use: the server reads `?status=open,paid` as
equality with the literal `open,paid`, never as a list. The same goes
for a column whose own name ends in `_in`: on a `check_in` column, with
no `check` column, `filters: { check_in: [...] }` throws and asks for
`check_in_in`.

`watch` uses fetch streaming (not `EventSource`: it can't send the
Authorization header). Batch rollbacks (HTTP 400 with a decodable
envelope) resolve normally with `committed: false`. Every response body
the client buffers — the parse in `do()` and the `ApiError`
snapshot on the error path — goes through a capped read (`_readBody`,
1 MiB, the same `maxBodyBytes` the Go client enforces): a bigger body
throws instead of buffering. `watch` holds one SSE line, and one event's
joined `data:` lines, to the same cap and rejects past it. Every one of
these caps counts UTF-8 bytes, not string length, so text outside ASCII
gets no more room than the Go client gives it.

Bodies and `watch` events parse with the client's `parseJSON`, not bare
`JSON.parse`: an integer past 2^53 (a snowflake id, a big counter)
arrives as its exact decimal string where `JSON.parse` would round it.
Every other value parses the same. `client.d.ts` types each integer
field `number | string` for that reason, and the server takes the string
back for an integer column, so a value read this way round-trips.

A field that is not required, has no generated value and is not part of
the key can hold SQL `NULL`, and the server returns it as `null`, so
`client.d.ts` types it `T | null` on `<Struct>` and on `<Struct>Input`
(where sending `null` clears it). A required field, a generated one and
the key stay plain `T`. Check for `null` before using an optional field's
value. The exported
`<entity>Fields` constants map camelCase names to the snake_case column
names query params require.

Each entity is a property of `Client` named for its table (`api.posts`),
so a table whose name the class already uses is refused: a `token`
table would replace the bearer token with its resource. The reserved
names are `Client`'s own (`baseURL`, `token`, `do`, `_fetch`,
`_readBody`, `_sse`) and the `Object` prototype's (`constructor`,
`toString`, `valueOf`, `hasOwnProperty`, `isPrototypeOf`,
`propertyIsEnumerable`, `toLocaleString`). Rename the table.

Each entity also names interfaces in `client.d.ts`: `<Struct>`,
`<Struct>Input` and `<Struct>Patch`, plus `<Struct>Moves` when it has
transitions and `<Struct>Endpoints` when it has SDK endpoints, where
`<Struct>` is the entity name in PascalCase. The generator refuses an
entity whose name is already declared, by the runtime (`Resource`,
`Client`, `CursorPage`, `ListResponse`, `ListParams`, `CursorParams`,
`BatchResult`, `BatchResponse`, `ApiError`) or by another entity
(`order_input` beside `order`'s `OrderInput`): TypeScript would merge the
two interfaces into one, or fail on the generic `CursorPage<T>`. Rename
the entity.

A `Blob` (a `File` is one) as the value of an Image or File field sends
that create, update or patch as multipart/form-data, every part named
by its column; `client.d.ts` types those fields `string | Blob`. A
multipart body cannot carry `null`, so clear a field in a separate JSON
patch. A file over 32 MiB, or a body over 64 MiB, throws before
anything is sent. A batch goes as JSON, so `batchCreate` and
`batchUpdate` reject a `Blob` value and their item types leave it out:
upload one row at a time.

```js
const logo = document.querySelector("input[type=file]").files[0];
const created = await api.customers.create({ name: "Acme", logo });
```

A composite-key entity names a record by an array of its key values in
key order, which `client.d.ts` types as a readonly tuple:

```js
await api.enrollments.get([studentId, courseId]);
await api.enrollments.patch([studentId, courseId], { grade: "B" });
await api.enrollments.batchUpdate([{ studentId, courseId, grade: "C" }]);
await api.enrollments.batchDelete([[studentId, courseId]]);
```

Each value goes into its own path segment, escaped the way the server's
`entity.JoinKey` escapes it, so the id the request names is the one the
server stores. An entity with an integer `id` types it `number | string`,
like every integer field (see below).

## The casing contract both SDKs encode

Responses are camelCase (the server default). Request bodies accept snake
or camel. **Filter/sort query parameters and validation-error `fields`
keys are always the snake_case column names.** The generated READMEs
repeat this so consumers stop guessing.

## Flags and project configuration

```
gofastr generate sdk [--target=go,js] [--out=gen/sdk] [--name=<app>]
    [--api-prefix=api] [--only=a,b] [--exclude=c] [--module=<path>]
    [--base-url=<url>] [--sdk-version=<semver>] [--no-archive]
    [--dry-run] [--json]
```

Defaults can live in the `codegen:` section of `gofastr.settings.yml` as
a generator entry named
`sdk` (flags win). The same entry runs under `gofastr generate --config`
via the built-in in-process generator, the first generator that ships
built into the codegen registry:

```yaml
codegen:
  output: gen
  generators:
    - name: sdk
      output: sdk
      config:
        targets: [go, js]
        name: myapp
        base_url: "https://api.example.com"
        only: [posts, comments]
```

The config keys are `name`, `api_prefix`, `module`, `base_url`,
`sdk_version` (strings) and `targets`, `only`, `exclude` (YAML lists).
A flag passed empty still wins: `--only=` generates every entity and
`--api-prefix=` means no prefix, whatever the settings entry says.
An
unknown key, a value of the wrong type, or a comma string where a list
belongs fails the run with the entry named, from `gofastr generate`,
`gofastr build` and `gofastr generate sdk` alike.
`gofastr generate sdk` takes defaults only from the settings file in the
directory it runs in, since the entry's paths are relative to that file.

Config-provenance strings are treated as hostile at emission: the app
name (flag or `gofastr.settings.yml`) is scrubbed before it reaches the
generated READMEs — control bytes and fence characters are stripped, and
in the copyable install fence the `-sdk` directory hint is reduced to the
module-path alphabet — so a cloned config cannot add lines or shell
content to the README that ships inside the artifacts. Entity tables
must also derive a plain JavaScript identifier for the `<entity>Fields`
constants in `client.js` (the docs site serves that file to browsers);
the build refuses a table that does not.

## Serving: the SDK docs site

`framework/sdkdocs` mounts a public docs site (default `/docs/api`) with
install guides (tabbed Go / JS-TS / curl snippets via `ui.CodeTabs`),
download buttons, an auth guide (minting `gfsk_` tokens), an errors
reference, and one live reference page per entity: fields, endpoints,
query params, and examples rendered **from the live registry on every
request**, so the reference cannot drift from the running API.

```go
site := app.NewApp("My App")
// … uihost.New / fwApp.Mount …
err := sdkdocs.Mount(site, fwApp.Router(), sdkdocs.Config{
    Registry:     fwApp.Registry,
    CRUDMounted:  fwApp.EntityCRUDMounted,
    Artifacts:    os.DirFS("gen/sdk/dist"), // or an embed.FS for single-binary deploys
    BaseURL:      "https://your-app.example.com",
    APIPrefix:    "api",
    HasAPITokens: true,
})
```

Pass `CRUDMounted: fwApp.EntityCRUDMounted`. The site documents an entity
only when its CRUD routes were actually registered, and `Exposure.CRUD`
alone cannot answer that: an app with no DB mounts no CRUD at all while
every entity still reads `nil` ("auto"). Without the predicate the site
publishes a reference page, and an SDK download, for paths that answer
404. A columned view registered with `App.View` counts as mounted — it
serves read routes, and read routes are what the site documents. Leaving
the field nil keeps the older `Exposure`-only behaviour for callers that
document a registry without an App.

It is deliberately not a `uihost` option: the screens compose
`framework/ui`, which uihost must never import (its always-on styles
would leak into every host's CSS bundle).

Visibility is fail-closed: by default only `Public` entities are
documented; a gated entity's reference URL 404s exactly like a
nonexistent one. Opt in with an explicit `Entities` allow-list or
`IncludeGated: true`, and gate the whole site (screens, downloads and
the mobile section-menu drawer's `/chrome` and `/state` routes) with
`Policy`. A redirecting policy refuses the drawer's fetches with 403;
the runtime fetches them, so a redirect has nowhere to land. Hidden
fields never render.

Download routes live under `<BasePath>/sdk/`: `go.zip` (attachment),
`client.js` (inline: importable from the URL), `client.d.ts`, and
`manifest.json`. Responses carry `Cache-Control: no-cache` plus an ETag
from the manifest hash, so stable URLs revalidate for free.
The downloads follow the same visibility as the pages: every route
answers 404 unless each entity the manifest's `entities` list names is
one the site documents, and a missing manifest refuses too. Each file
must match the SHA-256 the manifest records for it, so an artifact an
earlier run left in dist (a `sdk-go.zip` from before a `--target=js`
run) answers 404 instead of serving schema the manifest no longer
names.
`gofastr generate sdk` covers every entity by default, so for a site
that documents only `Public` entities generate with
`--only=<those entities>`, or widen `Entities` / `IncludeGated` if
the gated schema is meant to ship. The overview page says when the
downloads are withheld, and one WARN names the entities at fault.

### Drift detection

Both halves compute the same schema hash over the covered entities
(`framework/sdk.SchemaHash`). The generator records it in the manifest;
the site recomputes it from the live registry. A mismatch logs one WARN
and shows a banner on every docs page; downloads keep working (a stale
SDK beats none), and re-running `gofastr generate sdk` clears it. A
manifest from a different gofastr version downgrades the check to an
"unknown provenance" note instead of a false stale warning.

The hash covers what changes a generated client: each entity's name,
table, primary-key columns in their declared order (keyed calls take the
key as positional arguments in that order), exposure, soft delete,
search fields, relations, states, settings-declared endpoints and every
non-hidden field. Reordering a composite key therefore reports drift,
since an old SDK would pass the key arguments the wrong way round.

The manifest must list the entities it covers: `ReadManifest` refuses an
empty (or omitted) `entities` list the same way it refuses a missing
artifact, because a zero-entity manifest makes this check vacuous — the
recomputed hash would be a constant no matter what the live registry
holds, and a generation run that lost its entity list would report
"current" forever after.

### Static export

Docs pages export through `App.ExportStatic` like any screens (entity
pages expand via `StaticPaths`). To ship the artifacts in a static tree,
point the builder's generic `ExtraDirs` at the dist directory:

```go
b := &static.Builder{Host: host, OutDir: out,
    ExtraDirs: map[string]fs.FS{"/docs/api/sdk": os.DirFS("gen/sdk/dist")}}
```

Files the export already produced (or the user static dir) always win.

## Common mistakes

- **Baking the API prefix into the docs/download URLs.** The docs site
  and the artifact downloads mount on the app host (`/docs/api/...`);
  only API calls live under `APIPrefix` (`/api/posts`). The SDK
  `baseURL` must include the prefix; the download URLs must not.
- **Filtering with camelCase names.** `?statusId_gte=` matches nothing:
  filter/sort query params and validation-error `fields` keys are the
  snake_case column names. The JS SDK exports `<entity>Fields`
  constants; the Go README shows the same rule.
- **Sending a bare key that reads two ways.** With fields `stage` and
  `stage_in`, `?stage_in=` is a 400: it could be `stage_in`'s equality
  or `stage`'s IN list. Name the operator in brackets after the whole
  field name: `filters: { "stage_in[eq]": "x" }` or
  `filters: { "stage[in]": ["a", "b"] }`. Both READMEs list every such
  key in the app, and the CLI's filter flags already send the bracket
  form for them.
- **Hand-editing `gen/sdk/` output.** The CLI has a `custom.go` seam;
  SDK output has no owned files: every regeneration overwrites in place.
  Wrap the generated client from your own package instead.

## Not covered (v1)

Endpoints registered by hand in Go get no typed methods: both SDKs expose
the `Do`/`do` escape hatch and the reference pages list them. Declare an
endpoint in `gofastr.settings.yml` to get a typed method
([Custom endpoints](endpoints.md)). Servers reconfigured to snake_case responses (`WithJSONCase`) should set
`sdkdocs.Config.SnakeCase` for the docs examples; the generated SDKs
assume the camelCase default.
