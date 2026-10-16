package main

import (
	"regexp"
	"strings"
	"testing"
)

func TestScreenMockUsesFrameworkComponents(t *testing.T) {
	h := string(screenMock())
	for _, want := range []string{
		"fui-badge--success", "fui-badge--neutral", // ui.StatusBadge
		`data-cui-comp="ui-data-table"`, // ui.DataTable, not a hand-built <table>
		`data-cui-comp="ui-card"`,       // ui.Card frames the screen
		"Acme Corp", "$8,900", "churned", "meridian.local/customers",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("screen mock missing %q\n%s", want, h)
		}
	}
	if strings.Contains(h, "mock-badge") {
		t.Fatalf("screen mock must not recreate framework badge styling\n%s", h)
	}
}

// The home page is the framework's showcase on the stock theme: every class
// in its markup must be one a kit component emits (fui-* parts, the
// DataTable's is-* variants, the highlighter's tk-* tokens). A site-invented
// class renders unstyled now that the site ships no stylesheet.
func TestHomeMarkupUsesOnlyKitClasses(t *testing.T) {
	out := string((&HomeScreen{}).Render())
	classRe := regexp.MustCompile(`class="([^"]*)"`)
	for _, m := range classRe.FindAllStringSubmatch(out, -1) {
		for _, c := range strings.Fields(m[1]) {
			if strings.HasPrefix(c, "fui-") || strings.HasPrefix(c, "is-") || strings.HasPrefix(c, "tk-") {
				continue
			}
			t.Errorf("home markup carries non-kit class %q", c)
		}
	}
	if strings.Contains(out, `style="`) {
		t.Error("home markup carries an inline style attribute")
	}
}

// Copy and links survive the rebuild onto kit components.
func TestHomeKeepsCopyAndLinks(t *testing.T) {
	out := string((&HomeScreen{}).Render())
	for _, want := range []string{
		"you or your agents",
		`href="/get-started"`, `href="/docs/"`,
		"Numbers you can check.", "MCP tools per entity", "npm packages",
		"Server-rendered screens, not just an API.",
		`href="/primitives"`, `href="/framework"`, `href="/agents"`,
		`href="/interactivity"`, `href="/generator"`, `href="/examples"`,
		`href="https://barcode.donaldmurillo.com/"`, `rel="external"`,
		`href="/examples#meridian"`,
		"go install github.com/DonaldMurillo/gofastr/cmd/gofastr@",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("home page lost %q", want)
		}
	}
}
