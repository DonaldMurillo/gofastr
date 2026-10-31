# Bugbot rules for cmd/gofastr

- Generated code is one-shot owned output. On a name collision the
  generator fails with a precise error; flag any auto-renaming.
- The generator never overwrites `custom.go`.
- Specs can come from URLs, so treat every spec- or app-spec-derived
  string as untrusted. Each one that lands in generated Go source must be
  validated or `%q`-quoted. `toCamelCase` transforms a name; it does not
  validate it.
- The app generator ships zero CSS. Flag generated markup or styles that
  a `framework/ui` component should provide.
- A new generator feature goes into the union app spec at
  `cmd/gofastr/internal/appgen/testdata/union/gofastr.yml`, or CI's
  affected-only run never builds it.
