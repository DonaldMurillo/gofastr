# Test apps

Whole apps that the framework's own tests, gates and evals drive. They
are fixtures, not examples: the reference apps are `examples/meridian`
and `examples/site`. Each one stays here because something depends on
it; when nothing does, delete it.

| App | Driven by |
|---|---|
| `ecommerce` | The app generator's end-to-end flagship (`flagship_test.go` regenerates and boots it), Meridian's app spec gate, `gofastr pack` tests, and entity and CRUD tests. |
| `lms`, `portfolio`, `real-estate` | App specs only. `TestExampleAppSpecsLoad` / `TestExampleAppSpecsBoot` and the security tests in `cmd/gofastr` generate and boot them. |
| `processmodule-demo` | The process-module go/no-go gate (`framework/processmodule*_test.go`, `framework/processmoduletest`) and its CI job. |
| `desktop-notes` | The `battery/desktop` dogfood app: `cmd/gofastr` desktop tests and the native `desktop_e2e` suite. |
| `webmcp-remote-assist` | The contract analyzers' fixtures and `core-ui/runtime`'s RTC e2e test; the WebMCP + WebRTC reference in `webmcp.md`. |
| `tracker`, `acme-site`, `layoutlab` | Tree layouts: `framework/ui`'s recipe tests, `framework/headless`'s e2e, and the code `layouts.md` and `ui-composition-recipes.md` quote. `layoutlab` holds the layout runtime's test fixtures. |
| `hn` | The primitives eval's `hn-*` tasks (`evals/primitives`), and the first-load numbers in `runtime-minification.md`. |

Run any of them the same way as an example: `go run ./internal/testapps/<name>`.
