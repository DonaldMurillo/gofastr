# GoFastr examples

Two reference apps. Each is kept deliberately: if a framework change
breaks one of them, the change is wrong.

| Example | What it is |
|---|---|
| [`meridian`](meridian) | The flagship SaaS app: a billing and revenue console with a marketing site, sign-in, owner-scoped CRUD, roles, an admin back office, scoped API tokens, and a generated customer CLI under `meridian/cmd/meridian` (`go install ./examples/meridian/cmd/meridian`; docs at `gofastr docs app-cli`). It was seeded from [`meridian/gofastr.yml`](meridian/gofastr.yml) and has been hand-evolved since. It is also the design-system screenshot reference (AGENTS.md hard rule 9). |
| [`site`](site) | The framework's docs site and component gallery: every UI component on its own page, the reference docs, and the gofastr-plugins registry rendered from a vendored `plugins.json` (`scripts/vendor-plugins-json.sh` refreshes it). It is what GitHub Pages serves. |

Run one under the dev server for rebuild-on-save, livereload, and the dev
MCP tools your coding agent reads:

```bash
cd examples/meridian && gofastr dev      # or: go run ./examples/meridian
cd examples/site && gofastr dev
```

`$PORT` (injected by `gofastr dev` and by PaaS runtimes) wins over each
app's default port. `TestE2E_DevLoop_Examples` in `cmd/gofastr` boots both
under `gofastr dev`, and `TestExamplesAreHMRReady` here checks that both
carry the livereload client.

## Where the other apps went

Apps that exist to be driven by the framework's own tests (the app
generator's specs, the process-module gate, the layout and desktop
suites, the eval targets) live in [`internal/testapps`](../internal/testapps).
They are fixtures, not starting points.
