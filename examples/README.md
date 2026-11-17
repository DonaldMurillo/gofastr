# GoFastr examples

Two kinds of example live here.

## Go examples (runnable)

Self-contained Go programs. Run one under the dev server for
rebuild-on-save, livereload, and the dev MCP tools your coding agent
reads:

```bash
cd examples/<name> && gofastr dev        # or: go run ./examples/<name>
```

Every server example binds through `isolation.ListenAddr`, so `$PORT`
(injected by `gofastr dev` and by PaaS runtimes) wins over the example's
default port. `TestE2E_DevLoop_Examples` in `cmd/gofastr` boots each one
under `gofastr dev` and checks it answers on the address the banner
prints. The process module is the exception: it is a stdio child, not a
server, so `go run` it.

| Example | What it shows |
|---|---|
| `meridian` | The flagship SaaS app (generated, then owned): auth + owner-scoped CRUD, scoped API tokens, and a generated customer CLI under `meridian/cmd/meridian`. Install it with `go install ./examples/meridian/cmd/meridian`; docs at `gofastr docs app-cli`. |
| `desktop-notes` | The same app as a desktop window (`battery/desktop`, experimental, macOS today) or over HTTP with `--serve`: owner-scoped notes, native menu, clipboard and file dialogs through the typed bridge, a plugin capability. |
| `processmodule-demo` | A process-isolated third-party module speaking the `moduleproto` protocol over stdio. The canonical [process-module](../framework/docs/content/process-modules.md) example and the child the go/no-go gate suite drives end to end. |
| `webmcp-remote-assist` | Authenticated WebMCP + WebRTC remote support: support-only tool discovery, one typed command behind the manual button and the AI tools, role-filtered realtime state, peer-to-peer camera with server-side signaling only. |
| `site` | The framework's live component gallery and reference docs site: every UI component rendered one per page, the hosted examples, and the gofastr-plugins registry rendered from a vendored `plugins.json` (`scripts/vendor-plugins-json.sh` refreshes it). Run with `cd examples/site && gofastr dev`. |
| `hn` | A Hacker News reader in the [HNPWA](https://hnpwa.com/) shape, for comparing first loads with other frameworks: five ranked lists, story pages with full comment threads, user pages. Synthetic seeded data in SQLite through the entity layer, `ui.EntryList` and `ui.CommentThread`, no app CSS beyond its top bar. See [`hn/README.md`](hn/README.md). |
| `layoutlab` | Tree-layout outlets prototype a shell layout with outlets, a route area, and per-screen/group fills. |

## App spec examples (declarative)

These are **`gofastr.yml` app specs**, no Go. They describe a whole app
(entities, screens, nav, seed) declaratively. Generate a runnable project
from one with:

```bash
cd examples/ecommerce && gofastr generate app gofastr.yml
```

| App spec | Domain |
|---|---|
| `ecommerce` | **The flagship.** A complete storefront: 5 related entities, themed UI, custom endpoints, and seed data, declared once in `gofastr.yml` and emitted as runnable Go. Ships secure-by-default: auth enabled plus owner-scoped `orders` / `order_items`. See [`ecommerce/README.md`](ecommerce/README.md); `flagship_test.go` regenerates and boots it end-to-end. |
| `lms` | Courses, lessons, enrollments. |
| `portfolio` | Projects + case studies. |
| `real-estate` | Listings, agents, inquiries. |

Every app spec here is validated by `TestExampleAppSpecsLoad`
(`cmd/gofastr`), so a broken one fails CI.
