package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/codegen"
)

func TestRenderAppSpecBlockTypes(t *testing.T) {
	cases := []struct {
		block appspec.AppSpecBlock
		want  string
	}{
		{appspec.AppSpecBlock{Type: "p", Text: "hi"}, "html.Paragraph(html.TextConfig{Class: \"\"}, render.Text(\"hi\"))"},
		{appspec.AppSpecBlock{Type: "text", Text: "hi", Class: "lead"}, "html.Paragraph(html.TextConfig{Class: \"lead\"}, render.Text(\"hi\"))"},
		{appspec.AppSpecBlock{Type: "heading", Level: 2, Text: "H"}, "html.Heading"},
		{appspec.AppSpecBlock{Type: "h3", Text: "H"}, "Level: 3"},
		{appspec.AppSpecBlock{Type: "h6", Text: "H"}, "Level: 6"},
		{appspec.AppSpecBlock{Type: "link", Href: "/x", Text: "L"}, "ui.Link"},
		{appspec.AppSpecBlock{Type: "section", Text: "S", Class: "c"}, "section"},
		{appspec.AppSpecBlock{Type: "weird", Text: "D"}, "html.Div(html.DivConfig{Class: \"\"}, render.Text(\"D\"))"},
		{appspec.AppSpecBlock{Type: "weird", Text: "D", Class: "k"}, "html.Div(html.DivConfig{Class: \"k\"}, render.Text(\"D\"))"},
	}
	for _, c := range cases {
		got := renderAppSpecBlockForScreen(appspec.AppSpec{}, appspec.AppSpecScreen{}, c.block, nil, nil, "/api")
		if !strings.Contains(got, c.want) {
			t.Errorf("block %q → %s, want substring %q", c.block.Type, got, c.want)
		}
	}
}

func TestAppSpecActionNameAndEvent(t *testing.T) {
	if appSpecActionName(appspec.AppSpecAction{Name: "Save"}) != "Save" {
		t.Fatal("explicit name")
	}
	if appSpecActionName(appspec.AppSpecAction{Event: "Submit"}) != "submit" {
		t.Fatal("event fallback")
	}
	if appspec.AppSpecActionEvent(appspec.AppSpecAction{}) != "click" {
		t.Fatal("default click")
	}
	// Empty/only-separator name falls back to the "screen" sentinel id.
	if got := screenActionComponentID(appspec.AppSpecScreen{Name: "/"}); got != "screen-screen" {
		t.Fatalf("sentinel id = %q", got)
	}
	if got := screenActionComponentID(appspec.AppSpecScreen{Name: "My Page"}); !strings.HasPrefix(got, "screen-") {
		t.Fatalf("id = %q", got)
	}
}

// ── generate.go printCodegenFilesJSON ─────────────────────────────────

func TestPrintCodegenFilesJSON(t *testing.T) {
	out := clitest.CovT_capStdout(t, func() {
		printCodegenFilesJSON([]codegen.GeneratedFile{
			{Path: "x.go", Content: "abc"},
			{Path: "y.go", Content: "de"},
		})
	})
	if !strings.Contains(out, `"path":"x.go"`) || !strings.Contains(out, `"size":3`) {
		t.Fatalf("json: %s", out)
	}
}
