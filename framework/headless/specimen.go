package headless

import "github.com/DonaldMurillo/gofastr/core/render"

// SpecimenGlyph is the icon a fixture draws when a component takes one.
//
// It is a real 16×16 svg, because "<svg/>" is not. An svg with no
// width, no height and no viewBox has no intrinsic size, so CSS falls
// back to the replaced-element default of 300×150 — and every fixture
// in this package once used the short form. A badge meant to be a
// pill drew 340px wide and an alert's header grew a 140px hole between
// its title and its text.
//
// Nothing in the markup was wrong. The classes were right, the parts
// were right, the audit was clean, and the page still looked broken —
// which is the whole reason a fixture has to be something a caller
// would plausibly pass, not the shortest string that type-checks.
const SpecimenGlyph render.HTML = `<svg width="16" height="16" viewBox="0 0 16 16" fill="none" ` +
	`stroke="currentColor" stroke-width="1.5" aria-hidden="true">` +
	`<circle cx="8" cy="8" r="6.25"/><path d="M5.5 8.25 7.25 10l3.25-3.5"/></svg>`
