# GoFastr examples

Two kinds of example live here.

## Go examples (runnable)

Self-contained Go programs — `go run ./examples/<name>`:

| Example | What it shows |
|---|---|
| `blog` | Entities in Go + custom endpoints + full-text search; the canonical starter. |
| `meridian` | The flagship SaaS app (blueprint-generated, then owned): auth + owner-scoped CRUD, scoped API tokens, and a generated customer CLI under `meridian/cmd/meridian` — `go install ./examples/meridian/cmd/meridian`, docs at `gofastr docs app-cli`. |
| `api-tour` | The v2 REST API: includes, cursor pagination, batch, SSE. |
| `backoffice` | The entity admin (`battery/admin`) behind a demo login. |
| `spa` | Client-side navigation over server-rendered islands. |
| `static-site` | Static page serving with the file server. |
| `embed-demo` | Semantic search with `battery/embed`. |

## Blueprint examples (declarative)

These are **`gofastr.yml` blueprints** — no Go. They describe a whole app
(entities, screens, nav, seed) declaratively. Generate a runnable project
from one with:

```bash
cd examples/ecommerce && gofastr generate --from=gofastr.yml
```

| Blueprint | Domain |
|---|---|
| `ecommerce` | **The flagship.** A complete storefront — 5 related entities, themed UI, custom endpoints, seed data — declared once in `gofastr.yml` and emitted as runnable Go. Ships secure-by-default: auth enabled plus owner-scoped `orders` / `order_items`. See [`ecommerce/README.md`](ecommerce/README.md); `flagship_test.go` regenerates and boots it end-to-end. |
| `lms` | Courses, lessons, enrollments. |
| `portfolio` | Projects + case studies. |
| `project-manager` | Projects, tasks, teams. |
| `real-estate` | Listings, agents, inquiries. |

Every blueprint here is validated by `TestExampleBlueprintsLoad`
(`cmd/gofastr`), so a broken one fails CI.
