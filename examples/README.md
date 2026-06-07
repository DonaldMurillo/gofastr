# GoFastr examples

Two kinds of example live here.

## Go examples (runnable)

Self-contained Go programs — `go run ./examples/<name>`:

| Example | What it shows |
|---|---|
| `blog` | Entities in Go + custom endpoints + full-text search; the canonical starter. |
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
gofastr generate --from examples/ecommerce/gofastr.yml
```

| Blueprint | Domain |
|---|---|
| `ecommerce` | Products, carts, orders. |
| `lms` | Courses, lessons, enrollments. |
| `portfolio` | Projects + case studies. |
| `project-manager` | Projects, tasks, teams. |
| `real-estate` | Listings, agents, inquiries. |

Every blueprint here is validated by `TestExampleBlueprintsLoad`
(`cmd/gofastr`), so a broken one fails CI.
