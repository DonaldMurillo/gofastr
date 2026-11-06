package sitesearch

import (
	"fmt"
	"testing"
)

func TestExtractSeparatesBlocks(t *testing.T) {
	for _, tag := range []string{"address", "fieldset", "form", "legend", "caption", "hgroup", "main", "header", "menu"} {
		t.Run(tag, func(t *testing.T) {
			content := fmt.Sprintf("<%[1]s>support@example.com</%[1]s><%[1]s>sales@example.com</%[1]s>", tag)
			if tag == "caption" {
				content = "<table>" + content + "</table>"
			}
			page := extract(t, "/contacts", "<main>"+content+"</main>")
			if len(page.Sections) != 1 || page.Sections[0].Text != "support@example.com sales@example.com" {
				t.Fatalf("block text = %+v, want separated contacts", page.Sections)
			}
			found := Build([]Page{page}).Search("sales", 10)
			if len(found) != 1 {
				t.Errorf("search for the second block found %d pages, want one", len(found))
			}
		})
	}
}

func TestExtractKeepsInlineWord(t *testing.T) {
	page := extract(t, "/inline", "<main>inter<span>oper</span><em>ability</em></main>")
	if len(page.Sections) != 1 || page.Sections[0].Text != "interoperability" {
		t.Fatalf("inline text = %+v, want one word", page.Sections)
	}
}
