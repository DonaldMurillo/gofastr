package print

import (
	"strings"
	"testing"
)

func TestPageCSSFromConfig(t *testing.T) {
	cases := []struct {
		name string
		page PageConfig
		want string
	}{
		{"a4 portrait", PageConfig{Size: A4, Orientation: Portrait, Margin: MM(12)},
			"@page { size: A4; margin: 12mm 12mm 12mm 12mm; }"},
		{"a4 landscape", PageConfig{Size: A4, Orientation: Landscape, Margin: MM(10)},
			"@page { size: A4 landscape; margin: 10mm 10mm 10mm 10mm; }"},
		{"letter", PageConfig{Size: Letter, Orientation: Portrait, Margin: MM(20)},
			"@page { size: Letter; margin: 20mm 20mm 20mm 20mm; }"},
		{"legal landscape", PageConfig{Size: Legal, Orientation: Landscape, Margin: MM(5)},
			"@page { size: Legal landscape; margin: 5mm 5mm 5mm 5mm; }"},
		{"custom", PageConfig{Size: Custom, CustomWidth: "80mm", CustomHeight: "auto", Margin: MM(2)},
			"@page { size: 80mm auto; margin: 2mm 2mm 2mm 2mm; }"},
		{"asymmetric margins", PageConfig{Size: A4, Margin: Margins{Top: "1in", Right: "2cm", Bottom: "10pt", Left: "5px"}},
			"@page { size: A4; margin: 1in 2cm 10pt 5px; }"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pageCSS(c.page); got != c.want {
				t.Errorf("pageCSS() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestCustomLengthRejected(t *testing.T) {
	got := pageCSS(PageConfig{
		Size:         Custom,
		CustomWidth:  "80mm;}body{display:none}@page{",
		CustomHeight: "auto",
	})
	if strings.Contains(got, "display:none") {
		t.Fatalf("CSS injection survived: %q", got)
	}
	if !strings.Contains(got, "210mm auto") { // width fell back to default
		t.Errorf("expected width fallback, got %q", got)
	}
}

func TestLinksAppCSSWhenSet(t *testing.T) {
	out := renderShell(shellInput{Title: "t", AppCSSHref: "/__gofastr/app.css", BaseCSS: "x", PageCSS: "y"})
	if !strings.Contains(out, `<link rel="stylesheet" href="/__gofastr/app.css">`) {
		t.Errorf("app.css link missing: %q", out)
	}
}

func TestNoAppCSSWhenUnset(t *testing.T) {
	out := renderShell(shellInput{Title: "t", BaseCSS: "x", PageCSS: "y"})
	if strings.Contains(out, "<link rel=\"stylesheet\"") {
		t.Errorf("unexpected stylesheet link: %q", out)
	}
}

func TestNeverLinksRuntimeJS(t *testing.T) {
	out := renderShell(shellInput{Title: "t", BaseCSS: "x", PageCSS: "y", AutoPrintSrc: "/print/__autoprint.js"})
	if strings.Contains(out, "runtime.js") {
		t.Errorf("print shell must never link runtime.js: %q", out)
	}
}

func TestAutoPrintUsesExternalScript(t *testing.T) {
	out := renderShell(shellInput{Title: "t", BaseCSS: "x", PageCSS: "y", AutoPrintSrc: "/print/__autoprint.js"})
	if !strings.Contains(out, `<script src="/print/__autoprint.js"></script>`) {
		t.Errorf("expected external autoprint script: %q", out)
	}
	if strings.Contains(out, "window.print") {
		t.Errorf("auto-print must not be inline (CSP): %q", out)
	}
}

func TestEffectivePageInherits(t *testing.T) {
	def := PageConfig{Size: A4, Orientation: Portrait, Margin: MM(12)}
	// Document overrides only the margin.
	got := effectivePage(&PageConfig{Margin: MM(20)}, def)
	if got.Size != A4 || got.Orientation != Portrait {
		t.Errorf("size/orientation not inherited: %+v", got)
	}
	if got.Margin.Top != "20mm" {
		t.Errorf("margin not overridden: %+v", got.Margin)
	}
}
