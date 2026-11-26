package agentsinv

import "testing"

func TestWhitespaceMarkdownReportedMissing(t *testing.T) {
	Reset(t)
	t.Cleanup(func() { Reset(t) })
	Register(Entry{Name: "blank", Kind: KindBattery, Markdown: "\n \t"})
	if len(All()) != 0 || len(MissingMarkdown()) != 1 {
		t.Fatal("blank Markdown was treated as a usable inventory entry")
	}
}
