package mcp

import (
	"strings"
	"testing"
)

// Property: every DATA field WidgetDocument interpolates is escaped for
// its own template context, so a hostile value cannot break out of the
// attribute it lands in. appdoc_test's breakout test covers Title
// (HTML-text) and RootID (attribute); Lang is the third interpolation —
// the <html lang="..."> attribute — and was unexercised. The ScriptURL
// is the builder's own constant, so Lang, Title and RootID are the whole
// attacker-reachable surface (Body and Script are author-owned content
// and pass through by documented contract).
func TestWidgetDocLangCannotBreakAttribute(t *testing.T) {
	breakout := "en\" onload=\"alert(1)'><script>alert(2)</script>"
	doc := WidgetDocument{
		Title: "T",
		Lang:  breakout,
		Body:  "<p>b</p>",
	}
	html, err := doc.HTML()
	if err != nil {
		t.Fatal(err)
	}

	// The payload must not manufacture script markup: the document holds
	// exactly the two script elements the builder itself emits (client +
	// inline), never a third.
	if n := strings.Count(html, "<script"); n != 2 {
		t.Errorf("SECURITY: [XSS] Lang payload %q produced %d script elements, want 2:\n%s", breakout, n, html)
	}
	// Nor an event handler attribute: the breakout's quote must have been
	// escaped inside the attribute value instead of closing it.
	if strings.Contains(html, `onload="alert`) {
		t.Errorf("SECURITY: [XSS] Lang payload escaped its attribute and injected an event handler:\n%s", html)
	}
	// The html element stays well-formed: exactly one lang attribute,
	// opened and closed by the builder's own quotes.
	if n := strings.Count(html, "<html lang=\""); n != 1 {
		t.Errorf("Lang payload broke the <html> tag shape (%d lang attributes):\n%s", n, html)
	}
}
