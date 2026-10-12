package ownstyle

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func defaultTokens(t *testing.T) map[string]string {
	t.Helper()
	return style.ThemeToTokens(style.DefaultTheme())
}

// The bad inbox file from the design walkthrough. 1817 is a Go+CSS
// rule (slice 5); it is not expected here.
const inboxCSS = `:scope { padding: 16px; color: #52525B; }
.title { font-size: var(--text-xxl); }
.banner { display: none !important; }
@media (min-width: 900px) { :scope { padding: 0; } }
.pulse { animation: pulse 1s infinite; }
@keyframes pulse { 50% { opacity: .4; } }
.card { background: var(--color-surface-soft); }   /* .card is passed to ui.Card's Class */
.card .fui-card__body { gap: 0; }
`

func TestCheckInboxExample(t *testing.T) {
	diags := Check("inbox.style.css", inboxCSS, KindScoped, defaultTokens(t))
	want := []string{
		"1:19 error GOFASTR1807 16px is --spacing-lg; write var(--spacing-lg)",
		"1:32 error GOFASTR1807 #52525B is --color-text-muted; write var(--color-text-muted)",
		"2:25 error GOFASTR1806 --text-xxl is not a theme token (did you mean --text-2xl?)",
		"3:25 error GOFASTR1811 !important: owned sheets load in the design system's order; specificity never needs it",
		"4:20 error GOFASTR1812 900px is not a theme breakpoint; use (--above-md) 768px or (--above-lg) 1024px",
		"5:10 warn GOFASTR1813 animation with no @media (--reduced-motion) block",
		"8:7 error GOFASTR1810 .fui-card__body is a kit class (.cui-*, .hui-*, .fui-*); style the content you pass into the slot instead",
	}
	got := make([]string, len(diags))
	for i, d := range diags {
		got[i] = d.String()
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("diagnostics differ.\n--- want ---\n%s\n--- got ---\n%s",
			strings.Join(want, "\n"), strings.Join(got, "\n"))
	}
}

func TestCheckWorkedExamplesAreClean(t *testing.T) {
	for _, tc := range goldenCases {
		t.Run(tc.file, func(t *testing.T) {
			src, err := os.ReadFile("testdata/" + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			diags := Check(tc.file, string(src), tc.kind, defaultTokens(t))
			if len(diags) != 0 {
				t.Fatalf("expected zero diagnostics, got:\n%s", joinDiags(diags))
			}
		})
	}
}

func joinDiags(ds []Diagnostic) string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.String()
	}
	return strings.Join(out, "\n")
}

func TestCheckUnknownTokens(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		// Unknown name, no close suggestion.
		{".a{color:var(--zzz)}", "GOFASTR1806 --zzz is not a theme token"},
		// A fallback does not waive it: the fallback would paint forever.
		{".a{color:var(--zzz, red)}", "GOFASTR1806 --zzz is not a theme token"},
		// Declared in-file custom properties are not theme misses.
		{".a{--zzz:red;color:var(--zzz)}", ""},
		// Registered via @property, likewise.
		{"@property --zzz { syntax: \"<color>\"; }\n.a{color:var(--zzz)}", ""},
		// ui-/fui- override knobs are not theme tokens.
		{".a{color:var(--fui-density-control-h)}", ""},
		{".a{color:var(--ui-x)}", ""},
		// Strings and comments never match.
		{".a{content:\"var(--zzz)\"}", ""},
		{".a{color:red /* var(--zzz) */}", ""},
	}
	for _, tc := range cases {
		ds := Check("t.style.css", tc.src, KindScoped, defaultTokens(t))
		if tc.want == "" {
			if len(ds) != 0 {
				t.Errorf("%q: unexpected %s", tc.src, joinDiags(ds))
			}
			continue
		}
		if len(ds) != 1 || !strings.HasPrefix(ds[0].String(), "1:") || !strings.Contains(ds[0].Message, strings.TrimPrefix(tc.want, "GOFASTR1806 ")) {
			t.Errorf("%q: want one %q, got %s", tc.src, tc.want, joinDiags(ds))
		}
		if len(ds) == 1 && ds[0].Rule != RuleUnknownThemeToken {
			t.Errorf("%q: rule %s", tc.src, ds[0].Rule)
		}
	}
}

func TestCheckHardcodedTokenValues(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{".a{padding:16px}", "1:12 error GOFASTR1807 16px is --spacing-lg; write var(--spacing-lg)"},
		{".a{color:#52525B}", "1:10 error GOFASTR1807 #52525B is --color-text-muted; write var(--color-text-muted)"},
		// Shorthands and multi-token values differ from every token value.
		{".a{padding:8px 16px}", ""},
		{".a{margin:0}", ""},
		// Values no token carries are a MISSING token, not a bypass.
		{".a{padding:13px}", ""},
		// A var() with its fallback restating the token is the
		// degraded-mode copy: correct.
		{".a{padding:var(--spacing-lg, 16px)}", ""},
		// Category gate: 16px on a font-size is not a text token.
		{".a{font-size:16px}", ""},
		// !important does not change the value.
		{".a{padding:16px !important}", "1:12"},
		// A trailing comment is not part of the value a browser reads,
		// so this is still exactly the token value.
		{".a{padding:16px /* lg */}", "1:12"},
		// Line widths are stroke tokens.
		{".a{border-width:1px}", "1:17 error GOFASTR1807 1px is --stroke-thin; write var(--stroke-thin)"},
		{".a{outline-offset:2px}", "1:19 error GOFASTR1807 2px is --stroke-focus"},
		// A number is read the way the browser reads it: a missing
		// leading zero, a trailing zero or a sign do not hide a token.
		{".a{opacity:.6}", "1:12 error GOFASTR1807 .6 is --opacity-muted"},
		{".a{line-height:1.60}", "1:16 error GOFASTR1807 1.60 is --leading-relaxed"},
		{".a{letter-spacing:-.01em}", "1:19 error GOFASTR1807 -.01em is --tracking-snug"},
		{".a{padding:+16.0px}", "1:12 error GOFASTR1807 +16.0px is --spacing-lg"},
		// The logical spacing properties are spacing too.
		{".a{padding-inline:16px}", "1:19 error GOFASTR1807 16px is --spacing-lg"},
		{".a{margin-block-start:16px}", "1:23 error GOFASTR1807 16px is --spacing-lg"},
	}
	for _, tc := range cases {
		ds := Check("t.style.css", tc.src, KindScoped, defaultTokens(t))
		if tc.want == "" {
			if len(ds) != 0 {
				t.Errorf("%q: unexpected %s", tc.src, joinDiags(ds))
			}
			continue
		}
		found := false
		for _, d := range ds {
			if d.Rule == RuleHardcodedTokenValue && strings.HasPrefix(d.String(), tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: want %q, got %s", tc.src, tc.want, joinDiags(ds))
		}
	}
}

// A theme may spell a token's number any way the browser reads: a
// muted opacity of ".60" still owns 0.6 in a sheet.
func TestCheckThemeNumberSpelling(t *testing.T) {
	tokens := defaultTokens(t)
	if _, ok := tokens["opacity-muted"]; !ok {
		t.Fatal("token map has no opacity-muted")
	}
	tokens["opacity-muted"] = ".60"
	ds := Check("t.style.css", ".a{opacity:0.6}", KindScoped, tokens)
	for _, d := range ds {
		if d.Rule == RuleHardcodedTokenValue && strings.Contains(d.String(), "--opacity-muted") {
			return
		}
	}
	t.Fatalf("0.6 against a .60 token: want 1807 naming --opacity-muted, got %s", joinDiags(ds))
}

func TestCheckFallbackDrift(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		// 8px is spacing-md: the fallback teaches a value that exists.
		{".a{padding:var(--spacing-md, 8px)}", ""},
		// 1rem is the same length as 16px (spacing-lg): fine.
		{".a{padding:var(--spacing-lg, 1rem)}", ""},
		// .5s is 500ms... spacing only goes to 48px; use a duration.
		{".a{transition-duration:var(--duration-normal, 250ms)}", ""},
		{".a{transition-duration:var(--duration-normal, 400ms)}", "1:24"},
		{".a{border-width:var(--stroke-thin, 1px)}", ""},
		{".a{border-width:var(--stroke-thin, 2px)}", "1:17"},
		// Colour fallbacks are author's degraded-mode choices.
		{".a{color:var(--color-text, #18181B)}", ""},
		// Complex fallbacks are left alone.
		{".a{padding:var(--spacing-lg, var(--spacing-md))}", ""},
		// Unknown name is 1806, not drift.
		{".a{padding:var(--spacing-xxl, 8px)}", "GOFASTR1806"},
	}
	for _, tc := range cases {
		ds := Check("t.style.css", tc.src, KindScoped, defaultTokens(t))
		if tc.want == "" {
			if len(ds) != 0 {
				t.Errorf("%q: unexpected %s", tc.src, joinDiags(ds))
			}
			continue
		}
		if tc.want == "GOFASTR1806" {
			unknown := false
			for _, d := range ds {
				switch d.Rule {
				case RuleUnknownThemeToken:
					unknown = true
				case RuleFallbackDrift:
					t.Errorf("%q: an unknown name reported as drift: %s", tc.src, d)
				}
			}
			if !unknown {
				t.Errorf("%q: an unknown name with a fallback should be GOFASTR1806", tc.src)
			}
			continue
		}
		found := false
		for _, d := range ds {
			if d.Rule == RuleFallbackDrift && strings.HasPrefix(d.String(), tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: want drift at %q, got %s", tc.src, tc.want, joinDiags(ds))
		}
	}
}

func TestCheckKitClassAndAttributeSelectors(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{".card .fui-card__body { gap: 0; }", "1:7 error GOFASTR1810"},
		{".a .fui-sidebar__item { color: red; }", "1:4 error GOFASTR1810"},
		// The attribute form, positioned at the bracket.
		{".a [data-cui-scope=\"x\"] { color: red; }", "1:4 error GOFASTR1810"},
		// App-owned data attributes are fine.
		{".column[data-drop-over] { outline: red; }", ""},
		// Kit class mentioned in a string never matches.
		{".a{content:\".fui-card__body\"}", ""},
	}
	for _, tc := range cases {
		ds := Check("t.style.css", tc.src, KindScoped, defaultTokens(t))
		if tc.want == "" {
			if len(ds) != 0 {
				t.Errorf("%q: unexpected %s", tc.src, joinDiags(ds))
			}
			continue
		}
		if len(ds) != 1 || !strings.HasPrefix(ds[0].String(), tc.want) {
			t.Errorf("%q: want %q, got %s", tc.src, tc.want, joinDiags(ds))
		}
	}
}

func TestCheckRawMediaWidths(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"@media (min-width: 900px) { .a { color: red; } }", "1:20 error GOFASTR1812 900px is not a theme breakpoint"},
		{"@media (max-width: 900px) { .a { color: red; } }", "1:20"},
		{"@media screen and (min-width: 768px) { .a { color: red; } }", "1:31 error GOFASTR1812 768px is the md breakpoint; use (--above-md) so the theme can move it"},
		{"@media (--above-md) { .a { color: red; } }", ""},
		// Raw widths stay legal in @container.
		{"@container (min-width: 26rem) { .a { color: red; } }", ""},
		{"@media (hover: hover) { .a { color: red; } }", ""},
		{"@media (aspect-ratio: 16/9) { .a { color: red; } }", ""},
		// Range syntax counts too.
		{"@media (width >= 900px) { .a { color: red; } }", "1:18"},
	}
	for _, tc := range cases {
		ds := Check("t.style.css", tc.src, KindScoped, defaultTokens(t))
		if tc.want == "" {
			if len(ds) != 0 {
				t.Errorf("%q: unexpected %s", tc.src, joinDiags(ds))
			}
			continue
		}
		found := false
		for _, d := range ds {
			if d.Rule == RuleRawMediaWidth && strings.HasPrefix(d.String(), tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: want %q, got %s", tc.src, tc.want, joinDiags(ds))
		}
	}
}

func TestCheckAnimationReducedMotion(t *testing.T) {
	// The issuecard pattern: same selector inside the reduced-motion
	// block silences the warning.
	quiet := ":scope.fresh { animation: settle var(--duration-slow) 1; }\n" +
		"@media (--reduced-motion) { :scope.fresh { animation: none; } }\n"
	if ds := Check("t.style.css", quiet, KindScoped, defaultTokens(t)); len(ds) != 0 {
		t.Fatalf("unexpected %s", joinDiags(ds))
	}
	// No block: warning at the animation declaration's name.
	ds := Check("t.style.css", ".pulse { animation: pulse 1s infinite; }", KindScoped, defaultTokens(t))
	if len(ds) != 1 || ds[0].String() != "1:10 warn GOFASTR1813 animation with no @media (--reduced-motion) block" {
		t.Fatalf("got %s", joinDiags(ds))
	}
	// animation: none needs no off-switch.
	if ds := Check("t.style.css", ".a { animation: none; }", KindScoped, defaultTokens(t)); len(ds) != 0 {
		t.Fatalf("unexpected %s", joinDiags(ds))
	}
	// animation-name counts too.
	ds = Check("t.style.css", ".a { animation-name: pulse; }", KindScoped, defaultTokens(t))
	if len(ds) != 1 || ds[0].Rule != RuleAnimationWithoutReducedMotion {
		t.Fatalf("got %s", joinDiags(ds))
	}
}

func TestCheckAppSheetRules(t *testing.T) {
	scoped := KindScoped
	if ds := Check("t.style.css", "h2 { color: red; }\n.a { --x: 1; }", scoped, defaultTokens(t)); len(ds) != 0 {
		t.Fatalf("scoped sheets may select elements and declare custom properties: %s", joinDiags(ds))
	}
	cases := []struct {
		src  string
		want string
	}{
		{"p { color: red; }", "1:1 error GOFASTR1819"},
		{"h2.title { color: red; }", "1:1 error GOFASTR1819"},
		{":scope { color: red; }", "1:1 error GOFASTR1819"},
		{".a, p { color: red; }", "1:5 error GOFASTR1819"},
		{".priority.level--urgent { color: red; }", ""},
		{".a { --accent: #B91C1C; }", "1:6 error GOFASTR1819"},
	}
	for _, tc := range cases {
		ds := Check("t.style.css", tc.src, KindApp, defaultTokens(t))
		if tc.want == "" {
			if len(ds) != 0 {
				t.Errorf("%q: unexpected %s", tc.src, joinDiags(ds))
			}
			continue
		}
		if len(ds) != 1 || !strings.HasPrefix(ds[0].String(), tc.want) {
			t.Errorf("%q: want %q, got %s", tc.src, tc.want, joinDiags(ds))
		}
	}
}

func TestCheckTokenCustomProperty(t *testing.T) {
	ds := Check("t.style.css", ".a { --color-primary: #000; }", KindScoped, defaultTokens(t))
	if len(ds) == 0 || ds[0].Rule != RuleTokenCustomProperty {
		t.Fatalf("want 1820, got %s", joinDiags(ds))
	}
	if !strings.HasPrefix(ds[0].String(), "1:6 error GOFASTR1820 --color-primary is a theme token") {
		t.Fatalf("got %s", ds[0])
	}
	// A non-token custom property is fine in a scoped sheet.
	if ds := Check("t.style.css", ".a { --accent: #B91C1C; }", KindScoped, defaultTokens(t)); len(ds) != 0 {
		t.Fatalf("unexpected %s", joinDiags(ds))
	}
	// But declaring a theme-token-named property is refused in an app
	// sheet twice over (1819 and 1820).
	ds = Check("t.style.css", ".a { --color-primary: #000; }", KindApp, defaultTokens(t))
	rules := map[string]int{}
	for _, d := range ds {
		rules[d.Rule]++
	}
	if rules[RuleTokenCustomProperty] != 1 || rules[RuleAppSheetSelector] != 1 {
		t.Fatalf("want 1820+1819, got %s", joinDiags(ds))
	}
}

func TestCheckParseErrorsSurface(t *testing.T) {
	ds := Check("t.style.css", ".a { color: red", KindScoped, defaultTokens(t))
	if len(ds) == 0 || ds[len(ds)-1].Rule != RuleParseError {
		t.Fatalf("want a parse error, got %s", joinDiags(ds))
	}
	for _, d := range ds {
		if d.Severity != SeverityError {
			t.Errorf("parse diagnostic not an error: %s", d)
		}
	}
	_ = fmt.Sprint(ds)
}

func TestCheckAppSheetExemptsKeyframeSelectors(t *testing.T) {
	// Keyframe selectors (from/to/percentages) are not style-rule
	// selectors; the app sheet's class-only rule does not apply to
	// them (finding 3).
	src := ".a { animation: spin 1s; }\n@keyframes spin { to { rotate: 1turn; } 50% { opacity: .5; } }\n"
	ds := Check("t.style.css", src, KindApp, defaultTokens(t))
	for _, d := range ds {
		if d.Rule == RuleAppSheetSelector && (strings.Contains(d.Message, "to is not") || strings.Contains(d.Message, "50%")) {
			t.Fatalf("keyframe selector flagged: %s", d)
		}
	}
	// The animation itself needs its reduced-motion block — that still
	// fires in an app sheet.
	found := false
	for _, d := range ds {
		if d.Rule == RuleAnimationWithoutReducedMotion {
			found = true
		}
	}
	if !found {
		t.Errorf("1813 should still fire for the animation: %s", joinDiags(ds))
	}
}

func TestCheckAppSheetSubjectMessageAsWritten(t *testing.T) {
	// The :scope subject prints as written, with the app-scope reason
	// (finding 4).
	ds := Check("t.style.css", ":scope { color: red; }", KindApp, defaultTokens(t))
	if len(ds) != 1 || ds[0].Rule != RuleAppSheetSelector {
		t.Fatalf("diags: %s", joinDiags(ds))
	}
	if !strings.Contains(ds[0].Message, "app sheet: :scope ") {
		t.Fatalf("message mangles the selector text: %s", ds[0].Message)
	}
	if !strings.Contains(ds[0].Message, "the app owner has no root element; its scope is :root") {
		t.Fatalf("message misses the reason: %s", ds[0].Message)
	}
	// Compound pseudo subjects print as written too (":scope.fresh"),
	// and element subjects keep their own message.
	ds = Check("t.style.css", ":scope.fresh { color: red; }", KindApp, defaultTokens(t))
	if len(ds) != 1 || !strings.Contains(ds[0].Message, ":scope.fresh") {
		t.Fatalf("compound subject mangled: %s", joinDiags(ds))
	}
	ds = Check("t.style.css", "p { color: red; }", KindApp, defaultTokens(t))
	if len(ds) != 1 || !strings.Contains(ds[0].Message, "p is not a class selector") {
		t.Fatalf("element message changed: %s", joinDiags(ds))
	}
}

func TestCheckRawMediaWidthRemEmMatchesBreakpoint(t *testing.T) {
	// The kit's own sheets spell @media (min-width: 48rem): name the
	// breakpoint it equals (finding 6).
	ds := Check("t.style.css", "@media (min-width: 48rem) { .a { color: red; } }", KindScoped, defaultTokens(t))
	if len(ds) != 1 || ds[0].Rule != RuleRawMediaWidth {
		t.Fatalf("diags: %s", joinDiags(ds))
	}
	want := "48rem is 768px, the md breakpoint; use (--above-md)"
	if !strings.Contains(ds[0].Message, want) {
		t.Fatalf("message: %s (want %q)", ds[0].Message, want)
	}
	// A rem width that matches no breakpoint keeps the bracketing hint.
	ds = Check("t.style.css", "@media (min-width: 50rem) { .a { color: red; } }", KindScoped, defaultTokens(t))
	if len(ds) != 1 || !strings.Contains(ds[0].Message, "50rem is not a theme breakpoint") {
		t.Fatalf("non-matching rem message: %s", joinDiags(ds))
	}
	// 60rem = 960px: between md and lg.
	ds = Check("t.style.css", "@media (min-width: 60rem) { .a { color: red; } }", KindScoped, defaultTokens(t))
	if !strings.Contains(ds[0].Message, "(--above-md)") || !strings.Contains(ds[0].Message, "(--above-lg)") {
		t.Fatalf("bracketing message for rem: %s", joinDiags(ds))
	}
}
