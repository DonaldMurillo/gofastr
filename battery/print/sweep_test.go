package print

import (
	"strings"
	"testing"
)

func TestPDFFilenameKeepsOnlySafeBytes(t *testing.T) {
	b := New(Config{DefaultAccess: Public, PDFRenderer: &fakeRenderer{}}).
		Document(Document{Name: "inv oice", Path: "/inv/{id}", Build: docBuild("x")})
	rec := get(t, mount(t, b), `/print/inv/a%22%3B%20x=y%0D%0AZ/pdf`)
	if cd := rec.Header().Get("Content-Disposition"); cd != `inline; filename="inv-oice-a-x-y.pdf"` {
		t.Fatalf("Content-Disposition = %q, want only [A-Za-z0-9._-] in the stem", cd)
	}
}

func TestPrintBaseCSSHasTheColumnKnob(t *testing.T) {
	if !strings.Contains(printBaseCSS, "var(--ui-print-doc-max-width, 820px)") {
		t.Fatal("printBaseCSS lost the --ui-print-doc-max-width knob print.md documents")
	}
}
