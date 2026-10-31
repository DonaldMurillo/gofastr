# Bugbot rules for framework/ui

- Component configs carry `ExtraAttrs`, forwarded to the root element
  through `html.SafeExtraAttrs`: extras first, then the component's owned
  keys re-asserted, with `class`, `id` and `data-cui-*` dropped
  case-insensitively. Flag code that forwards `ExtraAttrs` raw outside the
  `extraAttrsRawLegacy` allow-list. The gates live in
  `framework/ui/extraattrs_contract_test.go`.
- SSR output must be byte-identical across runs. The `mapwriter` analyzer
  catches a map ranged while writing; also flag other sources of
  run-to-run drift in markup, such as goroutine ordering or unsorted
  results from a registry lookup.
- Styling goes through `registry.RegisterStyle` and theme tokens. A new
  variant belongs on the component's config, not in an override from the
  calling page.
