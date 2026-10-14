package interactive

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func mustPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		if r == nil {
			t.Fatalf("no panic; want one naming %q", want)
		}
		if msg, _ := r.(string); !strings.Contains(msg, want) {
			t.Fatalf("panic %v; want it to name %q", r, want)
		}
	}()
	f()
}

func TestSheetPillNeedsName(t *testing.T) {
	mustPanic(t, "requires Name", func() { SheetPill(SheetPillConfig{Opens: "w"}) })
}

func TestSheetPillNeedsOneOpener(t *testing.T) {
	mustPanic(t, "exactly one", func() { SheetPill(SheetPillConfig{Name: "Menu"}) })
	mustPanic(t, "exactly one", func() {
		SheetPill(SheetPillConfig{Name: "Menu", Opens: "w", Popover: "p"})
	})
}

func TestSheetPillOpensWidgetOrPopover(t *testing.T) {
	w := string(SheetPill(SheetPillConfig{Name: "Menu", Opens: "nav-sheet"}))
	if !strings.Contains(w, `data-cui-open="nav-sheet"`) || strings.Contains(w, "popovertarget") {
		t.Errorf("Opens should name the widget and nothing else:\n%s", w)
	}
	p := string(SheetPill(SheetPillConfig{Name: "Menu", Popover: "toc-sheet"}))
	if !strings.Contains(p, `popovertarget="toc-sheet"`) || strings.Contains(p, "data-cui-open") {
		t.Errorf("Popover should name the popover and nothing else:\n%s", p)
	}
}

// The label falls back to Name; the accessible name starts with Name.
func TestSheetPillLabelFallsBackToName(t *testing.T) {
	h := string(SheetPill(SheetPillConfig{Name: "On this page", Popover: "p"}))
	for _, want := range []string{
		`<span class="cui-sheet-pill__sr">On this page: </span>`,
		`<span class="cui-sheet-pill__label">On this page</span>`,
		`<span class="cui-sheet-pill__detail"></span>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q:\n%s", want, h)
		}
	}
}

func TestSheetPillUnknownSwapPanics(t *testing.T) {
	mustPanic(t, "SheetPill unknown SwapAt", func() {
		SheetPill(SheetPillConfig{Name: "Menu", Opens: "w", SwapAt: "xl"})
	})
}

// Each swap width hides the pill at and above it and pads the page
// below it; the default scope must not also match a pill with a swap.
func TestSheetPillCSSPerSwap(t *testing.T) {
	css := sheetPillCSS(style.Theme{})
	for _, want := range []string{
		`@media (min-width: 900px) {
  [data-cui-comp="cui-sheet-pill"]:where(:not(.cui-sheet-pill--swap-md, .cui-sheet-pill--swap-lg)) { display: none; }`,
		`@media (min-width: 48rem) {
  [data-cui-comp="cui-sheet-pill"]:where(.cui-sheet-pill--swap-md) { display: none; }`,
		`@media (max-width: 63.99rem) {
  body:has([data-cui-comp="cui-sheet-pill"]:where(.cui-sheet-pill--swap-lg):not(.cui-sheet-pill--inline))`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("CSS missing:\n%s", want)
		}
	}
}

func TestPhoneSheetNeedsIDAndTitle(t *testing.T) {
	mustPanic(t, "requires ID", func() { PhoneSheet(PhoneSheetConfig{Title: "T"}, "") })
	mustPanic(t, "requires Title", func() { PhoneSheet(PhoneSheetConfig{ID: "s"}, "") })
}

// The pill opens the sheet by id, the sheet is a popover dialog named
// by its title, and the close button hides that same popover.
func TestPhoneSheetWiresPillToPopover(t *testing.T) {
	h := string(PhoneSheet(PhoneSheetConfig{ID: "toc-sheet", Title: "On this page", SwapAt: SwapMD}, "<ol></ol>"))
	for _, want := range []string{
		`class="cui-phone-sheet cui-phone-sheet--swap-md"`,
		`class="cui-sheet-pill cui-sheet-pill--swap-md"`,
		`popovertarget="toc-sheet"`,
		`aria-labelledby="toc-sheet-title" class="cui-phone-sheet__sheet" id="toc-sheet" popover="auto" role="dialog"`,
		`<span class="cui-sheet-head__title" id="toc-sheet-title">On this page</span>`,
		`popovertarget="toc-sheet" popovertargetaction="hide"`,
		`<div class="cui-phone-sheet__body"><ol></ol></div>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q:\n%s", want, h)
		}
	}
}

// At and above the swap the sheet and its body step out of the box
// tree, so the body lays out in the sheet's parent.
func TestPhoneSheetCSSFlowsAboveSwap(t *testing.T) {
	css := phoneSheetCSS(style.Theme{})
	want := `@media (min-width: 48rem) {
  [data-cui-comp="cui-phone-sheet"]:where(.cui-phone-sheet--swap-md) .cui-phone-sheet__sheet,
  [data-cui-comp="cui-phone-sheet"]:where(.cui-phone-sheet--swap-md) .cui-phone-sheet__body { display: contents; }`
	if !strings.Contains(css, want) {
		t.Errorf("CSS missing:\n%s", want)
	}
}
