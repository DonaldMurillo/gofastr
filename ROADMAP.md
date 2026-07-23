# GoFastr — Roadmap

Forward-looking work that isn't built yet (or isn't finished yet). Shipped
features live in `framework/docs/content/<feature>.md` (also embedded into
the binary — run `gofastr docs` to browse) and the two architecture
documents (`framework/ARCHITECTURE.md`, `core-ui/ARCHITECTURE.md`).

Each section ends with a status note. When something ships, delete it from
here and add the `docs/<feature>.md` it now belongs in. Full design
sketches for deleted sections live in git history (this file was trimmed
of ~10 shipped sections on 2026-07-15).

---

## 1. API versioning

**Status:** EXPERIMENTAL (2026-05-22) — lives in
`framework/experimental/apiversions/` (`Version` route-group wrapper,
`Projection`/`ProjectionSet` per-version field shapes, deprecation
headers, version-namespaced MCP tools). Speculative without a real v1↔v2
in-tree case study to shape the projection machinery. Revisit — and
consider promoting out of experimental — when a real consumer surfaces
the shape.

---

## 2. Deferred UI components

Both shipped once as SSR shells with no runtime contract and were
deleted (2026-05-22). Re-add only with the full loop: runtime module +
RPC handler + an e2e test proving the interaction round-trips.

- **Calendar / date picker** — needs `core-ui/runtime/src/datepicker.js`
  + RPC + e2e asserting day selection works end-to-end.
- **Inline edit field** — needs `core-ui/runtime/src/inlineedit.js` +
  RPC + e2e proving click → input swap → Enter saves.

---

## 3. Validation & adoption — proving the thesis

**Status:** in progress — the declaration→surfaces proof shipped;
external adoption is open.

GoFastr makes a falsifiable bet: *an AI agent (or a human) can describe
a real CRUD-heavy app once, in a `gofastr.yml` blueprint, and get a
correct, inspectable, runnable app — SQL + REST + OpenAPI + MCP + UI —
without hand-writing the glue.*

1. **Framework stability → the `v1.0.0` gate.** The API may change until
   v1.0. Drop `v0.x` only when the gate below is green.
2. **Declaration-first proof.** ✓ Shipped: `examples/ecommerce` — a
   five-entity blueprint generated, built, and surface-tested end-to-end
   (`flagship_test.go`, zero hand-written app code).
3. **Dogfooding.** ✓ Kiln and `examples/site` are built on the framework.
   Deepen by porting more internal tooling onto blueprints.
4. **External adoption — the genuinely open item.** No outside
   production users yet. Recruit and evaluate one through the
   [external pilot program](docs/pilot-program.md); this is the part the code
   cannot prove for itself.

**`v1.0.0` gate** (what must be true to drop `v0.x`)

- The public `framework.X` + battery interfaces are frozen. The documented
  [deprecation policy](framework/docs/content/stability.md) is already in
  force; the remaining work is completing the freeze.
- The declaration→surfaces proof (#2) stays green in CI, and the
  remaining roadmap items are closed or consciously scoped out.
- At least one non-author app runs on GoFastr in a real setting (#4).
