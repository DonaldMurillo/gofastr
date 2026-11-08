# Codegen fixes: C04, C05, C11 (November 7, 2026)

Base: main at `7d8cd9eea`. Nothing committed. Shared checkout; only the
paths below were touched for these IDs.

## C04: SDK response shapes omit injected timestamps (fixed)

Root: the Go and TS response emitters and the JS `<entity>Fields`
constants walked `decl.Fields`, while Define adds `created_at` and
`updated_at` later.

Fix: `injectedTimestamps(decl)` in
`cmd/gofastr/internal/appgen/generate_client.go` returns the timestamp
columns Define would add (timestamps nil or true, name not declared).
`responseFields(decl)` appends them to the declared fields. The Go
response struct and the d.ts response interface walk `responseFields`;
`writeJSEntity` appends the stamps to `<entity>Fields` in client.js and
client.d.ts (the d.ts key is quoted: `emitident` refuses an unquoted
`%s:` slot fed by a struct field). Input and Patch still walk
`decl.Fields`, so no write shape gains a read-only column. A declared
`created_at` keeps its own type and flags.

Same `renderClient` writes the app's `entities/client/client.go`, so the
committed copies were regenerated (only the two stamp fields, gofmt
alignment and the marker sum changed):
`examples/meridian/entities/client/client.go` (the
`TestGeneratedTreesMatchGenerator` recipe, client.go copied alone) and
`examples/ecommerce/app/entities/client/client.go` (`gofastr generate
client` in a scratch copy).

## C05: TS relation to an integer key typed string (fixed)

Root: `tsTypeForField` maps `relation` to `string` without looking at
the target.

Fix: `tsFieldType(fd, decls)` in
`cmd/gofastr/internal/appgen/generate_sdkjs.go` resolves a single-row
relation to `tsKeyType(target)`, matching the target the way
`relationKeyField` does (name or table). An integer `id` target gives
`number | string`, the same as the target's `id`; a UUID or string key
gives `string`. Both the response interface and Input use it.
`writeJSEntity` now takes the spec's decls.

Limits: a target outside the generated selection (`--only`,
`--exclude`), a composite-key target and `many` relations stay `string`,
as before. The Go client's relation fields stay `string` (its decoder
reads a number key; not counted by the review). No `tsc` exists on this
machine, so no compiler diagnostic is claimed; node ran the generated
client.js.

## C11: pack writes data-table rows as strings (fixed)

Root: `writeYAMLListItem` in `cmd/gofastr/internal/packcmd/pack.go` had
no `[]any` case, so a row fell to `fmt.Sprint` ("[Ada Admin]").

Fix: an inner list is written as `- []` when empty, `- [a, b]` (through
`writeFlowList`, so commas, colons and quotes stay quoted) when it holds
only scalars, and as a bare `-` with the items indented below when it
holds maps or lists (core/yaml has no flow form for those). Maps and
scalars in lists are written as before.

## Tests

New:

- `TestPackTableRowsStayLists` (packcmd): public path. Generate an app
  from a spec with rows (one cell holding a comma), `RunPack -o file`,
  `appspec.LoadAppSpec` the file, compare rows deep-equal, regenerate
  and compare screen sources.
- `TestPackNestedListsRoundTrip` (packcmd): empty inner list, scalars
  needing quotes, inner list holding a map; whole-spec deep-equal.
- `TestSDKShapesMatchCRUDResponse` (appgen): real CRUD create on SQLite
  for five entities (int id, UUID id, relations to both, timestamps off,
  declared `created_at`). Every response key must have a Go struct tag
  and a d.ts member; Input/Patch carry no stamp; `gadgetsFields` names
  both stamps; relation types equal the target key types; node (skipped
  when absent) runs client.js against the captured response.

All three failed on the base before the fix. The timestamps-off guard
was broken on purpose and the test failed, then restored.

Updated: `TestClientTypesIntID` matches the id line by regexp (gofmt
realigns the struct now that it has `CreatedAt`);
`writeJSEntity` callers in `emitter_output_context_security_test.go`
and `noquery_generator_surfaces_test.go` pass the decl list.

Runs (all `-p 2 -count=1`):

```sh
go test -p 2 -count=1 ./cmd/gofastr/internal/appgen/ ./cmd/gofastr/internal/packcmd/
go test -p 2 -count=1 ./cmd/gofastr/internal/generatecmd/ ./cmd/gofastr/internal/buildcmd/
go test -p 2 -count=1 -run '^TestGeneratedTreesMatchGenerator$' ./examples/meridian/
go test -p 2 -count=1 ./framework/docs/
go build -p 2 ./examples/ecommerce/... ./examples/meridian/...
go vet -p 2 -vettool=<built cmd/vettool> ./cmd/gofastr/internal/appgen/ ./cmd/gofastr/internal/packcmd/
```

All pass. The review's retained probes now stop at "did not reproduce",
and the pack fixture's CLI chain (validate, generate, pack, validate,
generate) exits 0 with `- [Ada, Admin]` rows. packcmd's
generated-app builds (`VariantBuild`) were not forced with
`GOFASTR_TEST_ALL=1`; no Postgres run.

Docs: `framework/docs/content/sdk.md` (timestamps in response shapes and
`<entity>Fields`; relation key typing and its limits). No pack doc change:
the app-spec reference already says `rows` is a list of lists. No
CHANGELOG entry was written.

## Coordinator validation

The combined checkout passed fresh full `appgen` and `packcmd` suites.
After comment-only cleanup, focused `TestSDKShapesMatchCRUDResponse`,
`TestPackTableRowsStayLists` and `TestPackNestedListsRoundTrip` passed with
verbose output. Node was installed and its generated-client branch ran;
no TypeScript compiler result is claimed. Meridian's
`TestGeneratedTreesMatchGenerator` passed independently.

The first coordinator `framework/docs` rerun failed while fetching the
existing observability example's OpenTelemetry dependency: a TLS handshake
timeout at proxy.golang.org, not a generated SDK compilation diagnostic.
One fresh retry of the full docs package passed (8.708s). Standard `go vet`
also passed for i18n, appgen and packcmd; no repository dependency changed.
The repository's own vettool independently passed on those same packages.
