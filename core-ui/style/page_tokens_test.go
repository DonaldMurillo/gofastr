package style_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Every site needs a page column, a header height and a few font
// weights. Before these were tokens, the acme header copied 67.5rem,
// 3.5rem and 600 into its sheet because no var() could name them.

func TestPageTokensOnRoot(t *testing.T) {
	css := style.DefaultTheme().CSSCustomProperties()
	for _, decl := range []string{
		"--size-page-width: 66rem;",
		"--size-page-gutter: clamp(20px, 5vw, 32px);",
		"--size-header-height: 56px;",
		"--size-narrow-width: 640px;",
		"--size-content-width: 1080px;",
		"--size-wide-width: 1280px;",
		"--font-weight-normal: 400;",
		"--font-weight-medium: 500;",
		"--font-weight-semibold: 600;",
		"--font-weight-bold: 700;",
	} {
		if !strings.Contains(css, decl) {
			t.Errorf(":root is missing %q", decl)
		}
	}
}

func TestSizeAndWeightRefs(t *testing.T) {
	if got := (style.Size{Name: "page-width"}).CSS(); got != "var(--size-page-width)" {
		t.Errorf("Size.CSS() = %q", got)
	}
	if got := (style.FontWeight{Name: "bold"}).CSS(); got != "var(--font-weight-bold)" {
		t.Errorf("FontWeight.CSS() = %q", got)
	}
}

func TestPageTokensAutoFillNames(t *testing.T) {
	th := style.DefaultTheme()
	th.Layout.PageWidth.Name = ""
	th.FontWeights.Semibold.Name = ""
	style.AutoFillNames(&th)
	if th.Layout.PageWidth.Name != "page-width" {
		t.Errorf("PageWidth name = %q", th.Layout.PageWidth.Name)
	}
	if th.FontWeights.Semibold.Name != "semibold" {
		t.Errorf("Semibold name = %q", th.FontWeights.Semibold.Name)
	}
}

func TestValidateRejectsBadSizeAndWeight(t *testing.T) {
	cases := map[string]func(*style.Theme){
		"empty size":   func(th *style.Theme) { th.Layout.PageWidth.Value = "" },
		"zero weight":  func(th *style.Theme) { th.FontWeights.Bold.Value = 0 },
		"weight 1001":  func(th *style.Theme) { th.FontWeights.Bold.Value = 1001 },
		"bare word":    func(th *style.Theme) { th.Layout.HeaderHeight.Value = "tall" },
		"decl breaker": func(th *style.Theme) { th.Layout.HeaderHeight.Value = "56px;}" },
	}
	for name, mutate := range cases {
		th := style.DefaultTheme()
		mutate(&th)
		if err := th.Validate(); err == nil {
			t.Errorf("%s: Validate accepted it", name)
		}
	}
}

func TestApplyTokensSizeAndWeight(t *testing.T) {
	base := style.DefaultTheme()
	cases := []struct{ key, valid, invalid string }{
		{"size-page-width", "72rem", "72rem;}"},
		{"size-page-width", "min(100%, 72rem)", "wide"},
		{"size-header-height", "4rem", "4"},
		{"font-weight-bold", "700", "bold"},
		{"font-weight-bold", "800", "1001"},
		{"font-weight-normal", "400", "0"},
	}
	for _, c := range cases {
		if _, err := style.ApplyTokens(base, map[string]string{c.key: c.valid}); err != nil {
			t.Errorf("ApplyTokens(%q=%q): %v", c.key, c.valid, err)
		}
		if _, err := style.ApplyTokens(base, map[string]string{c.key: c.invalid}); err == nil {
			t.Errorf("ApplyTokens(%q=%q) accepted it", c.key, c.invalid)
		}
	}
	got, err := style.ApplyTokens(base, map[string]string{"size-page-width": "74rem", "font-weight-semibold": "650"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Layout.PageWidth.Value != "74rem" || got.FontWeights.Semibold.Value != 650 {
		t.Errorf("ApplyTokens did not write the slots: %q, %d", got.Layout.PageWidth.Value, got.FontWeights.Semibold.Value)
	}
}

// font-weight-bold belongs to font-weight, not to font: the checks pick
// which tokens may stand in for a property by category, and a weight
// must never be offered for font-family.
func TestTokenCategoryLongestPrefix(t *testing.T) {
	for key, want := range map[string]string{
		"font-weight-bold": "font-weight",
		"font-body":        "font",
		"size-page-width":  "size",
		"color-primary-fg": "color",
		"z-modal":          "z",
		"tk-kw":            "tk",
		"nope-x":           "nope",
		"nodash":           "nodash",
	} {
		if got := style.TokenCategory(key); got != want {
			t.Errorf("TokenCategory(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestResolveAllPageTokens(t *testing.T) {
	got := style.DefaultTheme().ResolveAll("{size.page-width} {font-weight.bold}")
	if got != "var(--size-page-width) var(--font-weight-bold)" {
		t.Errorf("ResolveAll = %q", got)
	}
}
