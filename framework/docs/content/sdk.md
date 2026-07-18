# Ship your API as SDKs

`gofastr generate sdk` turns your app's HTTP API into downloadable client
SDKs — a standalone, stdlib-only **Go module** and a zero-dependency
**JS/TS client** (one handrolled ESM `client.js` plus `client.d.ts`) —
and `framework/sdkdocs` serves them from the app itself behind a public
docs site with a live per-entity API reference.

```bash
cd your-app          # the directory holding entities/
gofastr generate sdk
```

Like `generate cli`, the entity set comes from your project source
(`entities/*.go`), not a blueprint. Unlike the CLI (one-shot owned code),
SDK output is **generator-owned and regenerates in place** — when the docs
site warns that downloads drifted from the live schema, the fix is
re-running the command.

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
detection). Archives are deterministic — regenerating an unchanged schema
produces byte-identical output.

Hidden fields never appear in any generated file: the SDKs are public
downloads, and a hidden column's name is schema the API deliberately
conceals.

### The Go SDK

The same typed client the generated app embeds (`entities/client`),
packaged as its own module (`module local/<app>-sdk` by default,
`--module` to override). Per entity: `List/Get/Create/Update/Patch/Delete`,
the three `_batch` verbs, and `Watch` (SSE), plus the `Do` escape hatch,
bearer-token auth (`c.Token = "gfsk_…"`), presence-aware pointer `Patch`
structs, and `*APIError` whose body carries the error envelope. Consumers
unzip and wire it locally:

```bash
curl -LO https://your-app.example.com/docs/api/sdk/go.zip
unzip go.zip
go mod edit -replace local/myapp-sdk=./myapp-sdk
```

### The JS/TS SDK

Two plain files, no npm packaging, no build step — publishing to a
registry is your business, not the generator's (add your own
package.json if you want it on npm). `client.js` is directly importable
from disk **or straight from the running app's URL**; `client.d.ts` types
it for TypeScript when the files sit side by side.

```js
import { Client, ApiError, postsFields } from "./client.js";

const api = new Client({ baseURL: "https://your-app.example.com/api", token });
const page = await api.posts.list({ sort: "-created_at", filters: { [postsFields.views + "_gte"]: 10 } });
await api.posts.patch(id, { views: 0 });   // JS objects are presence-faithful
await api.posts.watch((event, data) => console.log(event), { signal });
```

`watch` uses fetch streaming (not `EventSource` — it can't send the
Authorization header). Batch rollbacks (HTTP 400 with a decodable
envelope) resolve normally with `committed: false`. The exported
`<entity>Fields` constants map camelCase names to the snake_case column
names query params require.

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

Defaults can live in `gofastr.codegen.yml` as a generator entry named
`sdk` (flags win). The same entry runs under `gofastr generate --config`
via the built-in in-process generator — the first first-party generator in
the codegen registry:

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
```

## Serving: the SDK docs site

`framework/sdkdocs` mounts a public docs site (default `/docs/api`) with
install guides (tabbed Go / JS-TS / curl snippets via `ui.CodeTabs`),
download buttons, an auth guide (minting `gfsk_` tokens), an errors
reference, and one live reference page per entity — fields, endpoints,
query params, and examples rendered **from the live registry on every
request**, so the reference cannot drift from the running API.

```go
site := app.NewApp("My App")
// … uihost.New / fwApp.Mount …
err := sdkdocs.Mount(site, fwApp.Router(), sdkdocs.Config{
    Registry:     fwApp.Registry,
    Artifacts:    os.DirFS("gen/sdk/dist"), // or an embed.FS for single-binary deploys
    BaseURL:      "https://your-app.example.com",
    APIPrefix:    "api",
    HasAPITokens: true,
})
```

It is deliberately not a `uihost` option: the screens compose
`framework/ui`, which uihost must never import (its always-on styles
would leak into every host's CSS bundle).

Visibility is fail-closed: by default only `Public` entities are
documented; a gated entity's reference URL 404s exactly like a
nonexistent one. Opt in with an explicit `Entities` allow-list or
`IncludeGated: true`, and gate the whole site (screens AND downloads)
with `Policy`. Hidden fields never render.

Download routes live under `<BasePath>/sdk/`: `go.zip` (attachment),
`client.js` (inline — importable from the URL), `client.d.ts`, and
`manifest.json`. Responses carry `Cache-Control: no-cache` plus an ETag
from the manifest hash, so stable URLs revalidate for free.

### Drift detection

Both halves compute the same schema hash over the covered entities
(`framework/sdk.SchemaHash`). The generator records it in the manifest;
the site recomputes it from the live registry. A mismatch logs one WARN
and shows a banner on every docs page — downloads keep working (a stale
SDK beats none), and re-running `gofastr generate sdk` clears it. A
manifest from a different gofastr version downgrades the check to an
"unknown provenance" note instead of a false stale warning.

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
- **Filtering with camelCase names.** `?statusId_gte=` matches nothing —
  filter/sort query params and validation-error `fields` keys are the
  snake_case column names. The JS SDK exports `<entity>Fields`
  constants; the Go README shows the same rule.
- **Hand-editing `gen/sdk/` output.** Unlike `generate cli`'s
  `custom.go` seam, SDK output has no owned files — every regeneration
  overwrites in place. Wrap the generated client from your own package
  instead.

## Not covered (v1)

Custom entity `Endpoints` get no typed methods — both SDKs expose the
`Do`/`do` escape hatch and the reference pages list them. Multipart
uploads (Image/File fields) stay off the SDK surface, matching the CLI.
Servers reconfigured to snake_case responses (`WithJSONCase`) should set
`sdkdocs.Config.SnakeCase` for the docs examples; the generated SDKs
assume the camelCase default.
