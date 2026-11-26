package semantic

import (
	"strings"
	"testing"
)

func TestLangAwareSplitsGoTopLevelDecls(t *testing.T) {
	src := `package demo

// Add returns a+b.
func Add(a, b int) int { return a + b }

// Sub returns a-b.
func Sub(a, b int) int { return a - b }

type Point struct {
	X, Y int
}
`
	ck := NewLangAware()
	chunks, err := ck.Chunk(Document{ID: "demo", Source: "demo.go", Text: src})
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks (Add, Sub, Point), got %d:\n%+v", len(chunks), chunks)
	}
	joined := strings.Join(textsOf(chunks), "\n---\n")
	if !strings.Contains(joined, "func Add") || !strings.Contains(joined, "type Point") {
		t.Fatalf("chunks missing key decls:\n%s", joined)
	}
}

func TestLangAwareFallsBackOnParseError(t *testing.T) {
	ck := NewLangAware()
	chunks, err := ck.Chunk(Document{
		ID:     "broken",
		Source: "broken.go",
		Text:   "this is not valid go but the chunker should not panic",
	})
	if err != nil {
		t.Fatalf("Chunk fallback: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatalf("fallback produced no chunks")
	}
}

func TestLangAwareSplitsMarkdownByHeading(t *testing.T) {
	src := `Preamble line before any heading.

# Intro
Some intro content.

## Subsection
More content.

# Conclusion
The end.
`
	ck := NewLangAware()
	chunks, err := ck.Chunk(Document{ID: "doc1", Source: "guide.md", Text: src})
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) < 3 {
		t.Fatalf("expected >= 3 chunks for 1 preamble + 2 top sections, got %d", len(chunks))
	}
}

func TestLangAwareReChunksOverlongStructuralChunks(t *testing.T) {
	// A single giant func, structurally one chunk, but should be split
	// by the fallback because it exceeds MaxRunes.
	body := strings.Repeat("println(\"x\")\n", 200)
	src := "package big\n\nfunc Big() {\n" + body + "}\n"
	ck := &LangAware{MaxRunes: 64, Fallback: NewFixedWindow(80, 10)}
	chunks, err := ck.Chunk(Document{ID: "big", Source: "big.go", Text: src})
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("overlong func should be split; got %d chunks", len(chunks))
	}
	for _, c := range chunks {
		if len([]rune(c.Text)) > 80 {
			t.Fatalf("chunk exceeds fallback window: %d runes", len([]rune(c.Text)))
		}
	}
}

func textsOf(chunks []Chunk) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = c.Text
	}
	return out
}

func TestLangAwareRejectsInvalidUTF8InMarkdown(t *testing.T) {
	_, err := NewLangAware().Chunk(Document{ID: "doc", Source: "notes.md", Text: "# Heading\n\xff"})
	if err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
}

func TestLangAwareNonpositiveCapUsesDefault(t *testing.T) {
	body := "# Heading\n" + strings.Repeat("a", 700)
	l := &LangAware{MaxRunes: -1}
	chunks, err := l.Chunk(Document{ID: "doc", Source: "notes.md", Text: body})
	if err != nil || len(chunks) != 1 || chunks[0].Text != body {
		t.Fatalf("got %d chunks, %v; want a single structural chunk", len(chunks), err)
	}
}

func TestLangAwareEmptyCodeLanguageFallsBackToExtension(t *testing.T) {
	chunks, err := NewLangAware().Chunk(Document{
		ID: "doc", Source: "main.go", Text: "package main\nfunc a() {}\nfunc b() {}\n",
		Metadata: map[string]any{"kind": "code", "lang": ""},
	})
	if err != nil || len(chunks) != 2 {
		t.Fatalf("got %d chunks, %v; want two Go declarations", len(chunks), err)
	}
}
