# GoFastr roadmap

Forward-looking work that isn't built yet (or isn't finished yet). Shipped
features live in `framework/docs/content/<feature>.md` (also embedded into
the binary: run `gofastr docs` to browse) and the two architecture
documents (`framework/ARCHITECTURE.md`, `core-ui/ARCHITECTURE.md`).
Smaller tracked work, such as desktop platform gaps, lives in GitHub
issues.

Each section ends with a status note. When something ships, delete it from
here and document it in `framework/docs/content/<feature>.md`. Full design
sketches for deleted sections live in git history (this file was trimmed
of ~10 shipped sections on 2026-07-15).

---

## 1. API versioning

**Status:** EXPERIMENTAL (2026-05-22). Lives in
`framework/experimental/apiversions/` (`Version` route-group wrapper,
`Projection`/`ProjectionSet` per-version field shapes, deprecation
headers, version-namespaced MCP tools). Speculative without a real v1↔v2
in-tree case study to shape the projection machinery. Revisit, and
consider promoting out of experimental, when a real consumer surfaces
the shape.

---

## 2. Deferred UI components

- **Calendar component**
  ([issue 610](https://github.com/DonaldMurillo/gofastr/issues/610)). Dates use the native picker through
  `ui.DateField` and `ui.DateTimeField`, and that stays the recommended
  path (`framework/docs/content/form-module.md`, "Dates and times").
  `ui.DatePicker` covers what the native input cannot do: a range in one
  control, an always-visible calendar, disabled specific days, and data
  on the days. It enhances the native input (built on
  `headless.DatePicker`, bound by the `headless-datepicker` module, month
  changes an island answered by `ui.DatePickerMonth`), and its e2e tests
  post a picked day and a picked range through a real form.

**Status:** shipped. Not built, on purpose: a popup calendar for an
ordinary single date.

---

## 3. Validation & adoption: proving the thesis

**Status:** in progress. The declaration→surfaces proof shipped;
external adoption is open.

GoFastr makes a falsifiable bet: *an AI agent (or a human) can describe
a real CRUD-heavy app once, in a `gofastr.yml` app spec, and get a
correct, inspectable, runnable app, SQL + REST + OpenAPI + MCP + UI,
without hand-writing the glue.*

1. **Framework stability → the `v1.0.0` gate.** The API may change until
   v1.0. Drop `v0.x` only when the gate below is green. Every package
   already carries a support tier (`docs/public-api.md`), and none is
   Stable yet.
2. **Declaration-first proof.** ✓ Shipped: `examples/ecommerce`, a
   five-entity app spec generated, built, and surface-tested end-to-end
   (`flagship_test.go`, zero hand-written app code).
3. **Dogfooding.** ✓ Kiln, `examples/site`, and the `examples/meridian`
   flagship are built on the framework, and Meridian is the design
   system's completeness check. Deepen by porting more internal tooling
   onto app specs.
4. **External adoption, the genuinely open item.** No outside
   production users yet. Recruit and evaluate one through the
   [external pilot program](docs/pilot-program.md); this is the part the code
   cannot prove for itself.

**`v1.0.0` gate** (what must be true to drop `v0.x`)

- The public `framework.X` + battery interfaces are frozen. The documented
  [upgrade policy](framework/docs/content/stability.md) is already in
  force; the remaining work is completing the freeze.
- The declaration→surfaces proof (#2) stays green in CI, and the
  remaining roadmap items are closed or consciously scoped out.
- At least one non-author app runs on GoFastr in a real setting (#4).
