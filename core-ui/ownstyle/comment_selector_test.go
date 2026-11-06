package ownstyle

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestCompileSelectorCommentBoundaries(t *testing.T) {
	for _, test := range []struct{ source, selector string }{
		{".a/**/.b", ".a.b"},
		{".a /**/.b", ".a .b"},
		{".a/**/ .b", ".a .b"},
		{".fui/**/-card", ".fui/**/-card"},
		{"[data-cui/**/-rpc]", "[data-cui/**/-rpc]"},
		{"a/**/b", "a/**/b"},
		{"[value=1/**/px]", "[value=1/**/px]"},
		{"./**/a", ".a"},
	} {
		t.Run(test.source, func(t *testing.T) {
			sheet, diagnostics := Parse(test.source + "{color:red}")
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			compiled, err := Compile(sheet, "comments", KindScoped, style.ThemeToTokens(style.DefaultTheme()))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(compiled, test.selector+"{color:red}") {
				t.Fatalf("want selector %q; got %s", test.selector, compiled)
			}
		})
	}
}

func TestSelectorCommentCompoundModel(t *testing.T) {
	for _, test := range []struct {
		selector string
		flags    int
	}{
		{".card/**/.row", 1},
		{".card /**/.row", 0},
		{".card/**/ .row", 0},
	} {
		t.Run(test.selector, func(t *testing.T) {
			sheet, diagnostics := Parse(test.selector + "{gap:0}")
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			model, diagnostics := Model(sheet)
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			card := model.class("card")
			if card == nil || len(card.Flags) != test.flags {
				t.Fatalf("want card with %d flags; got %+v", test.flags, card)
			}
		})
	}
}

func TestSelectorCommentsCannotHideKitClass(t *testing.T) {
	for _, selector := range []string{".a/**/.fui-card", "./**/fui-card"} {
		diagnostics := Check("comments.style.css", selector+"{gap:0}", KindScoped, defaultTokens(t))
		found := false
		for _, diagnostic := range diagnostics {
			if diagnostic.Rule == RuleKitClassSelector {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: kit class not diagnosed: %v", selector, diagnostics)
		}
	}
}
