# Generated SDKs (Go and JavaScript/TypeScript)

`gofastr generate sdk` emits typed, zero-dependency Go and JavaScript/TypeScript client libraries from your app's entities (or from any OpenAPI 3 document), plus optional live SDK reference pages hosted by your app.

```bash
gofastr generate sdk                          # from entities/*.go
gofastr generate sdk --from-openapi api.json  # from an OpenAPI 3 document
```

## What gets generated

- **Go SDK:** stdlib-only client (`net/http`, `encoding/json`) with typed structs, request options, pointer-based patch payloads (so explicit zero values serialize properly), pagination helpers, batch operations, custom endpoints, and SSE event watchers.
- **JavaScript / TypeScript SDK:** fetch-based client with `.d.ts` type definitions, matching the mount's JSON wire casing (camelCase by default, decimals as strings, files/images as URLs).

Wire format rules match [Entity declarations](entity-declarations.md): `Hidden` fields are omitted, `ReadOnly` fields appear only on read structs, and `States`-managed fields are omitted from patch structs.

For OpenAPI schemas, nullable scalar responses and nullable scalar or object collection members use Go pointers, so `null` remains distinct from zero values. TypeScript includes `null` in the types of nullable array items and dictionary values.

## Typed repositories

For server-side Go code that wants to call your own app through the exact same typed interface without spinning up a network socket, the generated Go SDK includes local repository constructors backed by `app.InProcessHandler()`:

- Requests flow through the router, middleware, hooks, owner scoping, and validation in memory with zero TCP overhead.
- Identity comes from `r.Context()` (via `handler.SetUser` / tenant context), and `InProcessHandler()` refuses requests carrying cookies or bearer headers so callers cannot spoof HTTP credentials internally.

Use remote HTTP repositories for external services and CLIs, and local repositories for in-process workers, tests, and native desktop/mobile hosts (see [Application hosts](application-hosts.md)).

## Keeping the SDK in sync

Add an `sdk` entry under `codegen.generators` in [`gofastr.settings.yml`](settings.md) so `gofastr generate` and `gofastr build` regenerate the client automatically whenever entities change:

```yaml
version: 1
codegen:
  output: gen
  generators:
    - name: sdk
      output: sdk
```

For an OpenAPI-driven SDK, set `from_openapi: openapi.json` in the generator config (a local file in the repo, never a remote URL during builds).

### Drift detection

Both the generator and the hosted SDK docs site compute `framework/sdk.SchemaHash` over the covered entities. When the manifest hash disagrees with the live registry, the site logs a warning and shows a drift banner while keeping downloads working. The site caches the first check per mount — regenerate with `gofastr generate sdk` and redeploy or remount the site to check the new artifacts.

Artifacts with a missing or unreadable manifest, or a different manifest `schemaVersion`, show an "unknown provenance" banner (`gofastrVersion` records the generator version for display).

`framework/sdk.PackZip` accepts slash-separated relative prefixes and entry paths. It rejects backslashes, NUL bytes, and colons in the first path segment.

### Static export

SDK docs pages export through `App.ExportStatic` like any screen. Because the live download route serves the Go archive as `go.zip` while the generator writes `sdk-go.zip` in `dist/`, stage the files under their public names first:

```sh
mkdir -p gen/sdk/export
cp gen/sdk/dist/sdk-go.zip gen/sdk/export/go.zip
cp gen/sdk/dist/client.js gen/sdk/dist/client.d.ts gen/sdk/dist/manifest.json gen/sdk/export/
```

Then mount the staged directory on the static builder:

```go
b := &static.Builder{Host: host, OutDir: out,
	ExtraDirs: map[string]fs.FS{"/docs/api/sdk": os.DirFS("gen/sdk/export")}}
```

## Authentication

Generated clients authenticate with `Authorization: Bearer <token>` (matching `auth.TokenMiddleware` and scoped API tokens from `auth.NewTokensPlugin`) or custom headers configured on the client. The Go SDK and OpenAPI-generated JS/TS SDK refuse redirects by default (`redirect: "manual"`) so bearer tokens are never forwarded to another host; note that the entity-generated JS client uses Fetch's default redirect mode, so avoid serving untrusted 3xx redirects on entity API endpoints.

## Common mistakes

- **Editing generated SDK files by hand:** `gofastr generate` overwrites derived files. Wrap the generated client or add helpers in a separate file.
- **Mismatched `APIPrefix`:** know the three defaults: a handwritten `framework.NewApp()` mounts entity routes at the root (`""`) unless you pass `WithAPIPrefix`, app specs default to `api_prefix: api` (`/api`), and `gofastr generate sdk` uses an explicit `--api-prefix` flag or setting, then the app spec's prefix, and otherwise `""`. Pass a remote client base URL that includes whatever prefix your server mounts (or match `--api-prefix`), or every request will 404.
- **Passing `InProcessHandler()` to `http.ListenAndServe`:** `InProcessHandler()` is strictly for trusted in-process Go calls; use `app.Start` or `app.Serve` for network traffic.
- **Expecting `Hidden` fields in SDK structs:** hidden columns are never exposed on the wire or in generated clients.
