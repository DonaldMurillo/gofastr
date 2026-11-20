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

## Authentication

Generated clients authenticate with `Authorization: Bearer <token>` (matching `auth.TokenMiddleware` and scoped API tokens from `auth.NewTokensPlugin`) or custom headers configured on the client. Redirects are refused by default on authenticated requests so bearer tokens are never forwarded to another host.

## Common mistakes

- **Editing generated SDK files by hand:** `gofastr generate` overwrites derived files. Wrap the generated client or add helpers in a separate file.
- **Mismatched `APIPrefix`:** make sure the SDK's `--api-prefix` matches `AppConfig.APIPrefix` (default `/api`), or every request will 404.
- **Passing `InProcessHandler()` to `http.ListenAndServe`:** `InProcessHandler()` is strictly for trusted in-process Go calls; use `app.Start` or `app.Serve` for network traffic.
- **Expecting `Hidden` fields in SDK structs:** hidden columns are never exposed on the wire or in generated clients.
