package semantic

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// MaxRunes is a hard cap on every chunk LangAware returns, including
// text that goes straight to the fallback (unknown language, Go that
// does not parse).
func TestFallbackPathHonorsMaxRunes(t *testing.T) {
	l := &LangAware{MaxRunes: 16, Fallback: NewFixedWindow(32, 0)}
	for _, doc := range []Document{
		{ID: "txt", Source: "notes.txt", Text: strings.Repeat("abcdefghij", 10)},
		{ID: "go", Source: "broken.go", Text: "package x\nfunc (" + strings.Repeat("y", 90)},
	} {
		chunks, err := l.Chunk(doc)
		if err != nil {
			t.Fatalf("%s: %v", doc.ID, err)
		}
		if len(chunks) == 0 {
			t.Fatalf("%s: no chunks", doc.ID)
		}
		for _, c := range chunks {
			if n := utf8.RuneCountInString(c.Text); n > 16 {
				t.Fatalf("%s: chunk of %d runes over MaxRunes 16", doc.ID, n)
			}
		}
	}
}

type dropChunker struct{}

func (dropChunker) Chunk(Document) ([]Chunk, error) { return nil, nil }

// A fallback that returns nothing for an oversized section has dropped
// it; the rest of the document still chunks.
func TestEmptyFallbackDropsSection(t *testing.T) {
	l := &LangAware{MaxRunes: 40, Fallback: dropChunker{}}
	text := "# Short\nkept section\n# Long\n" + strings.Repeat("boilerplate ", 20)
	chunks, err := l.Chunk(Document{ID: "d", Source: "doc.md", Text: text})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || !strings.Contains(chunks[0].Text, "kept section") {
		t.Fatalf("got %d chunks, want only the short section: %+v", len(chunks), chunks)
	}
}
