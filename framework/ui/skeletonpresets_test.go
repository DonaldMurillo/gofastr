package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func TestSkeletonCard_RendersCardWithSkeletonLines(t *testing.T) {
	got := string(SkeletonCard(SkeletonCardConfig{}))
	checks := []string{
		"fui-card",           // wrapped in a Card surface
		"fui-skeleton-card",  // preset-specific class
		"fui-skeleton__line", // the bars
		`role="status"`,      // the one announcement
		`aria-hidden="true"`, // every bar visual-only
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("SkeletonCard output missing %q\nGOT: %s", want, got)
		}
	}
}

// The retired core-ui pattern's pinned properties, moved: the bars are
// hidden, and the announcement happens exactly once however many bars
// the preset draws (was TestAriaHiddenAlwaysTrue).
func TestSkeletonPresets_HiddenBarsOneAnnouncement(t *testing.T) {
	for name, h := range map[string]string{
		"card":   string(SkeletonCard(SkeletonCardConfig{BodyLines: 5, ShowFooter: true})),
		"row":    string(SkeletonRow(SkeletonRowConfig{})),
		"avatar": string(SkeletonAvatar(SkeletonAvatarConfig{})),
	} {
		if n := strings.Count(h, `role="status"`); n != 1 {
			t.Errorf("%s: %d live regions, wanted exactly one:\n%s", name, n, h)
		}
		bars := strings.Count(h, "fui-skeleton__line")
		hidden := strings.Count(h, `aria-hidden="true"`)
		if bars == 0 || hidden != bars {
			t.Errorf("%s: %d bars but %d hidden:\n%s", name, bars, hidden, h)
		}
	}
}

// The line count the caller asks for is the count the primitive draws
// and publishes on data-hui-lines (was TestCountRendersStack).
func TestSkeletonCard_BodyLines(t *testing.T) {
	got := string(SkeletonCard(SkeletonCardConfig{BodyLines: 4, ShowFooter: true}))
	if n := strings.Count(got, `class="fui-skeleton__line"`); n != 6 { // title + 4 body + footer
		t.Errorf("expected 6 bars (title + 4 body + footer), got %d:\n%s", n, got)
	}
	if !strings.Contains(got, `data-hui-lines="6"`) {
		t.Errorf("the primitive did not publish the line count:\n%s", got)
	}
}

// The footer line is the marked last line; the rhythm is the retired
// pattern's, pinned on the sheet (was TestStackLastLineShortened):
// the body's last line reads short (65%) with or without a footer,
// the footer itself is 35% under its own border, and body lines sit
// a tight rhythm apart.
func TestSkeletonCard_FooterOptional(t *testing.T) {
	withFooter := string(SkeletonCard(SkeletonCardConfig{ShowFooter: true}))
	withoutFooter := string(SkeletonCard(SkeletonCardConfig{}))

	if !strings.Contains(withFooter, "data-hui-skeleton-last") {
		t.Errorf("expected the marked last line when ShowFooter=true:\n%s", withFooter)
	}
	// The no-footer card still marks its last line (the primitive marks
	// it whenever there is more than one); the sheet shortens it.
	if strings.Count(withoutFooter, "fui-skeleton__line") != 3 {
		t.Errorf("default card is title + 2 body lines:\n%s", withoutFooter)
	}
	css := skeletonPresetsCSS
	for _, want := range []string{
		".fui-skeleton-card > .fui-skeleton__line[data-hui-skeleton-last]:nth-child(n+3)",
		".fui-skeleton-card--footed > .fui-skeleton__line[data-hui-skeleton-last]",
		".fui-skeleton-card--footed > .fui-skeleton__line:nth-last-child(3)",
		"gap: var(--spacing-sm, 4px)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("the sheet lost the pattern's card rhythm (%s):\n%s", want, css)
		}
	}
}

// A custom Label is the announcement the preset makes, and the
// default resolves through the request's language: the presets carry
// Label/Ctx for exactly this, and the one polite announcement is the
// invariant both paths keep.
func TestSkeletonPresets_LabelCustomAndLocalised(t *testing.T) {
	custom := string(SkeletonCard(SkeletonCardConfig{Label: "Loading the invoice"}))
	if !strings.Contains(custom, "Loading the invoice") {
		t.Errorf("a custom Label did not become the announcement:\n%s", custom)
	}
	if strings.Contains(custom, "Loading…") {
		t.Errorf("the default announcement replaced the custom Label:\n%s", custom)
	}

	ctx := makeCtxWithLocale(i18nui.KeyLoading, "Chargement…")
	localised := string(SkeletonRow(SkeletonRowConfig{Ctx: ctx}))
	if !strings.Contains(localised, "Chargement…") {
		t.Errorf("the default announcement did not resolve through the ctx locale:\n%s", localised)
	}
	for name, h := range map[string]string{"custom": custom, "localised": localised} {
		if n := strings.Count(h, `role="status"`); n != 1 {
			t.Errorf("%s: %d live regions, wanted exactly one:\n%s", name, n, h)
		}
	}
}

func TestSkeletonCard_RespectsCustomClass(t *testing.T) {
	got := string(SkeletonCard(SkeletonCardConfig{Class: "my-custom"}))
	if !strings.Contains(got, "my-custom") {
		t.Errorf("expected custom class in output, got: %s", got)
	}
}

func TestSkeletonRow_RendersLabelValueChevron(t *testing.T) {
	got := string(SkeletonRow(SkeletonRowConfig{}))
	checks := []string{
		"fui-skeleton-row",
		"fui-skeleton-row--chevron", // the sheet draws the chevron column
		"fui-skeleton__line",        // label + value
		`role="status"`,
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("SkeletonRow missing %q\nGOT: %s", want, got)
		}
	}
	if n := strings.Count(got, `class="fui-skeleton__line"`); n != 2 {
		t.Errorf("a row is exactly a label line and a value line, got %d:\n%s", n, got)
	}
}

func TestSkeletonRow_NoChevron(t *testing.T) {
	got := string(SkeletonRow(SkeletonRowConfig{HideChevron: true}))
	if strings.Contains(got, "fui-skeleton-row--chevron") {
		t.Errorf("expected no chevron modifier when HideChevron=true, got: %s", got)
	}
}

func TestSkeletonAvatar_RendersCircleAndLines(t *testing.T) {
	got := string(SkeletonAvatar(SkeletonAvatarConfig{}))
	if n := strings.Count(got, `class="fui-skeleton__line"`); n != 3 { // circle + name + sub
		t.Errorf("expected 3 bars (circle, name, sub), got %d:\n%s", n, got)
	}
	if sub := string(SkeletonAvatar(SkeletonAvatarConfig{HideSubline: true})); strings.Count(sub, `class="fui-skeleton__line"`) != 2 {
		t.Errorf("HideSubline collapses to circle + one line:\n%s", sub)
	}
}

func TestSkeletonCardExtraAttrsOnRoot(t *testing.T) {
	h := SkeletonCard(SkeletonCardConfig{ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("root missing data-test:\n%s", root)
	}
}

func TestSkeletonRowExtraAttrsOnRoot(t *testing.T) {
	h := SkeletonRow(SkeletonRowConfig{ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("root missing data-test:\n%s", root)
	}
}

func TestSkeletonAvatarExtraAttrsOnRoot(t *testing.T) {
	h := SkeletonAvatar(SkeletonAvatarConfig{ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("root missing data-test:\n%s", root)
	}
}

// The retired pattern's pinned stylesheet properties, moved: the
// shimmer keyframes, the reduced-motion opt-out, and the circle's
// equal sides (was TestStyleHasKeyframes + TestCircleUsesEqualSides).
func TestSkeletonPresets_CSSCarriesThePatternPins(t *testing.T) {
	for _, want := range []string{
		"@keyframes fui-skeleton-shimmer",
		"prefers-reduced-motion",
		".fui-skeleton-avatar > .fui-skeleton__line:first-child",
	} {
		if !strings.Contains(skeletonPresetsCSS, want) {
			t.Errorf("skeletonPresetsCSS missing %q", want)
		}
	}
	// Equal sides: the circle rule sets matching inline-size and
	// block-size (the pattern did it with an inline style the CSP
	// would have dropped; the sheet cannot be).
	circle := ".fui-skeleton-avatar > .fui-skeleton__line:first-child"
	at := strings.Index(skeletonPresetsCSS, circle)
	block := skeletonPresetsCSS[at : at+220]
	if !strings.Contains(block, "inline-size: 2.5rem") || !strings.Contains(block, "block-size: 2.5rem") {
		t.Errorf("the circle's sides are not equal in:\n%s", block)
	}
}

// The card's short last line and the footer's width are the retired
// pattern's numbers, proved by the captures: 65% for a body's last
// line, 35% for the footer.
func TestSkeletonCardKeepsThePatternWidths(t *testing.T) {
	css := skeletonPresetsCSS
	for _, want := range []string{
		".fui-skeleton-card > .fui-skeleton__line[data-hui-skeleton-last]:nth-child(n+3) {\n  inline-size: 65%;",
		".fui-skeleton-card--footed > .fui-skeleton__line[data-hui-skeleton-last] {\n  inline-size: 35%;",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("the card sheet lost a pattern width:\n%s\nin:\n%s", want, css)
		}
	}
}
