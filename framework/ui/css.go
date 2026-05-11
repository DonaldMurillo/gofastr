package ui

// BaseCSS returns the residual stylesheet for framework/ui — only the
// utility helpers that aren't owned by a registered component live
// here now. Every component-specific rule has been migrated to a
// registry.Style handle (see styles_*.go in this package). The
// runtime auto-loads each component's scoped CSS on first appearance
// via /__gofastr/comp/<name>.css.
//
// Apps still pass ui.BaseCSS() to WithCustomCSS for the helpers below.
// When this is empty, the call can be dropped entirely.
func BaseCSS() string {
	return `
/* ─── Visually hidden helper ─── */
.ui-visually-hidden {
  position: absolute !important;
  width: 1px; height: 1px;
  padding: 0; margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
`
}
