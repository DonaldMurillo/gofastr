package render

import (
	"strings"
	"testing"
)

// TestXSS_TextEscapesAngleBrackets verifies that user-controlled strings
// rendered via Text() have < and > escaped. Attack: injecting <script> tags.
func TestXSS_TextEscapesAngleBrackets(t *testing.T) {
	input := `<script>alert('xss')</script>`
	got := string(Text(input))
	if strings.Contains(got, "<script>") {
		t.Errorf("SECURITY: [xss] Text(%q) produced %q containing unescaped <script>. Attack: stored XSS via unescaped angle brackets.", input, got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("SECURITY: [xss] Text(%q) = %q — expected escaped angle brackets.", input, got)
	}
}

// TestXSS_TextEscapesDoubleQuotes verifies that double quotes in user input
// are escaped. Attack: breaking out of HTML attribute contexts.
func TestXSS_TextEscapesDoubleQuotes(t *testing.T) {
	input := `" onmouseover="alert(1)`
	got := string(Text(input))
	if strings.Contains(got, `" on`) {
		t.Errorf("SECURITY: [xss] Text(%q) produced %q with unescaped double quotes. Attack: attribute injection via unescaped quotes.", input, got)
	}
}

// TestXSS_TextEscapesSingleQuotes verifies that single quotes are escaped.
// Attack: breaking out of single-quoted HTML attributes.
func TestXSS_TextEscapesSingleQuotes(t *testing.T) {
	input := `' onfocus='alert(1)`
	got := string(Text(input))
	if !strings.Contains(got, "&#39;") {
		t.Errorf("SECURITY: [xss] Text(%q) = %q — single quotes not escaped. Attack: attribute breakout via unescaped single quotes.", input, got)
	}
}

// TestXSS_TextEscapesAmpersand verifies & is escaped to prevent entity
// injection. Attack: crafting &lt; entities that survive double-escaping.
func TestXSS_TextEscapesAmpersand(t *testing.T) {
	input := `&<script>`
	got := string(Text(input))
	if !strings.HasPrefix(got, "&amp;") {
		t.Errorf("SECURITY: [xss] Text(%q) = %q — ampersand not escaped first. Attack: entity injection via unescaped ampersand.", input, got)
	}
}

// TestXSS_AttrEscapesValue verifies that Attr() HTML-escapes the value.
// Attack: injecting event handlers through attribute values.
func TestXSS_AttrEscapesValue(t *testing.T) {
	input := `" onclick="alert(1)`
	got := Attr("class", input)
	if strings.Contains(got, `" onclick=`) {
		t.Errorf("SECURITY: [xss] Attr(\"class\", %q) = %q — value not escaped. Attack: attribute injection breaks out of value context.", input, got)
	}
}

// TestXSS_TagSanitizesUserContent verifies that Tag with user-controlled
// text content escapes it. Attack: nesting raw HTML inside a Tag.
func TestXSS_TagSanitizesUserContent(t *testing.T) {
	malicious := `<img src=x onerror=alert(1)>`
	got := string(Tag("div", nil, Text(malicious)))
	if strings.Contains(got, "<img ") {
		t.Errorf("SECURITY: [xss] Tag with Text(%q) = %q — raw HTML injected. Attack: XSS via unescaped user content inside Tag.", malicious, got)
	}
}

// TestXSS_TagNameInjection verifies that Tag with a malicious tag name
// doesn't break the HTML structure. Attack: injecting space/special chars
// into tag names to create new attributes.
func TestXSS_TagNameInjection(t *testing.T) {
	malicious := `div onclick="alert(1)"`
	got := string(Tag(malicious, nil, Text("hello")))
	// The tag name is not validated — this test documents the behavior.
	if strings.Contains(got, `onclick="alert(1)"`) {
		t.Errorf("SECURITY: [xss] Tag(%q, ...) produced %q — tag name injection created an event handler attribute. Attack: tag name breakout.", malicious, got)
	}
}

// TestXSS_RawBypassesEscaping documents that Raw() intentionally bypasses
// escaping. This is by design but must be tracked as a security-relevant API.
func TestXSS_RawBypassesEscaping(t *testing.T) {
	input := `<script>alert(1)</script>`
	got := string(Raw(input))
	if got != input {
		t.Errorf("Raw should not escape — got %q, want %q", got, input)
	}
	// Document: Raw is safe only when the caller controls the content.
	t.Logf("NOTE: [xss] Raw() is a security-relevant API — never pass user input to it")
}

// TestXSS_VoidTagNameInjection verifies that VoidTag with a malicious name
// doesn't inject attributes. Attack: script injection via tag name.
func TestXSS_VoidTagNameInjection(t *testing.T) {
	malicious := `img onerror="alert(1)"`
	got := string(VoidTag(malicious, map[string]string{"src": "x"}))
	if strings.Contains(got, `onerror="alert(1)"`) {
		t.Errorf("SECURITY: [xss] VoidTag(%q, ...) = %q — tag name injection created onerror attribute. Attack: void tag name breakout.", malicious, got)
	}
}
