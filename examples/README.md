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
| `blog` | Entities in Go + custom endpoints + full-text search; the canonical starter. |
| `meridian` | The flagship SaaS app (blueprint-generated, then owned): auth + owner-scoped CRUD, scoped API tokens, and a generated customer CLI under `meridian/cmd/meridian`. Install it with `go install ./examples/meridian/cmd/meridian`; docs at `gofastr docs app-cli`. |
| `api-tour` | The v2 REST API: includes, cursor pagination, batch, SSE. |
| `backoffice` | The entity admin (`battery/admin`) behind a demo login. |
| `spa` | Client-side navigation over server-rendered islands. |
| `static-site` | Static page serving with the file server. |
| `desktop-notes` | The same app as a desktop window (`battery/desktop`, experimental, macOS today) or over HTTP with `--serve`: owner-scoped notes, native menu, clipboard and file dialogs through the typed bridge, a plugin capability. |
| `desktop-focus` | The whole desktop surface in one app (`battery/desktop`, experimental, macOS today): hidden-title window, floating timer widget, settings window, tray countdown, notifications, deep links, cross-window messages, a `focus` plugin capability; also over HTTP with `--serve`. |
| `semantic-demo` | Semantic search with `battery/semantic`. |
| `embed-demo` | Embeddable surfaces: an app and a customer's site on two origins. |
| `processmodule-demo` | A process-isolated third-party module speaking the `moduleproto` protocol over stdio. The canonical [process-module](../framework/docs/content/process-modules.md) example and the child the go/no-go gate suite drives end to end. |
| `webmcp-remote-assist` | Authenticated WebMCP + WebRTC remote support: support-only tool discovery, one typed command behind the manual button and the AI tools, role-filtered realtime state, peer-to-peer camera with server-side signaling only. |
| `rtc-call` | WebRTC rooms the packaged way: `battery/rtc` signaling (zero hand-rolled relay), anonymous cookie identity through `Authorize`, the `rtc` runtime module with one peer connection per peer, peer-to-peer camera plus chat on a negotiated data channel. |
| `site` | The framework's live component gallery and reference docs site: every UI component rendered one per page, the hosted examples, and the gofastr-plugins registry rendered from a vendored `plugins.json` (`scripts/vendor-plugins-json.sh` refreshes it). Run with `cd examples/site && gofastr dev`. |

## Blueprint examples (declarative)

These are **`gofastr.yml` blueprints**, no Go. They describe a whole app
(entities, screens, nav, seed) declaratively. Generate a runnable project
from one with:

```bash
cd examples/ecommerce && gofastr generate --from=gofastr.yml
```

| Blueprint | Domain |
|---|---|
| `ecommerce` | **The flagship.** A complete storefront: 5 related entities, themed UI, custom endpoints, and seed data, declared once in `gofastr.yml` and emitted as runnable Go. Ships secure-by-default: auth enabled plus owner-scoped `orders` / `order_items`. See [`ecommerce/README.md`](ecommerce/README.md); `flagship_test.go` regenerates and boots it end-to-end. |
| `lms` | Courses, lessons, enrollments. |
| `portfolio` | Projects + case studies. |
| `project-manager` | Projects, tasks, teams. |
| `real-estate` | Listings, agents, inquiries. |

Every blueprint here is validated by `TestExampleBlueprintsLoad`
(`cmd/gofastr`), so a broken one fails CI.
