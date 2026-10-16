package semantic

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"unicode/utf8"
)

// LangAware is a structure-aware chunker that splits Go source on
// top-level declarations and Markdown on headings, falling back to a
// [FixedWindow] for anything it can't parse or for chunks that exceed
// MaxRunes.
//
// Routing key is Document.Metadata["kind"] when present; otherwise the
// Source extension drives the choice (.go → go, .md/.markdown → md).
type LangAware struct {
	// MaxRunes caps a single produced chunk in runes. Chunks above this
	// are re-chunked with FixedWindow. Defaults to 1024.
	MaxRunes int
	// Fallback is used for unknown kinds and for over-large structural
	// chunks. Defaults to NewFixedWindow(512, 64).
	Fallback Chunker
}

// NewLangAware returns a LangAware chunker with sensible defaults.
func NewLangAware() *LangAware {
	return &LangAware{
		MaxRunes: 1024,
		Fallback: NewFixedWindow(512, 64),
	}
}

// Chunk implements [Chunker]. The Fallback call runs under a recover
// net: a Fallback chunker is host-supplied pluggable code, and Chunk is
// reached from Index.Add and the watcher loop, which have no
// per-request recover — a panic becomes an attributed error instead of
// killing the process.
func (l *LangAware) Chunk(doc Document) ([]Chunk, error) {
	config := *l
	l = &config
	if l.MaxRunes == 0 {
		l.MaxRunes = 1024
	}
	if l.Fallback == nil {
		l.Fallback = NewFixedWindow(512, 64)
	}
	switch l.detectKind(doc) {
	case "go":
		if cs, ok := l.chunkGo(doc); ok {
			return l.flatten(cs)
		}
	case "md", "markdown":
		if cs, ok := l.chunkMarkdown(doc); ok {
			return l.flatten(cs)
		}
	}
	cs, err := chunkSafely(l.Fallback, doc)
	if err != nil {
		return nil, err
	}
	return l.bound(cs)
}

// bound splits any chunk over MaxRunes into MaxRunes windows, keeping
// offsets in the same frame as the input.
func (l *LangAware) bound(in []Chunk) ([]Chunk, error) {
	var out []Chunk
	for _, c := range in {
		if utf8.RuneCountInString(c.Text) <= l.MaxRunes {
			out = append(out, c)
			continue
		}
		parts, err := NewFixedWindow(l.MaxRunes, 0).Chunk(Document{ID: c.DocID, Source: c.Source, Text: c.Text, Metadata: c.Metadata})
		if err != nil {
			return nil, err
		}
		for j := range parts {
			parts[j].Offset[0] += c.Offset[0]
			parts[j].Offset[1] += c.Offset[0]
			parts[j].ID = chunkID(parts[j].DocID, parts[j].Offset[0], parts[j].Offset[1])
		}
		out = append(out, parts...)
	}
	return out, nil
}

// chunkSafely invokes one host-supplied Chunker under a recover net:
// chunkers are pluggable (Options.Chunker, LangAware.Fallback) and run
// on dispatch paths with no per-request recover, so a panicking chunker
// surfaces as an attributed error rather than a process kill.
func chunkSafely(c Chunker, doc Document) (chunks []Chunk, err error) {
	defer func() {
		if v := recover(); v != nil {
			chunks, err = nil, fmt.Errorf("semantic: chunker panicked: %v", v)
		}
	}()
	return c.Chunk(doc)
}

func (l *LangAware) detectKind(doc Document) string {
	if v, ok := doc.Metadata["kind"].(string); ok && v != "" {
		if v == "code" {
			if lang, ok := doc.Metadata["lang"].(string); ok {
				return lang
			}
		}
		if v == "doc" {
			if lang, ok := doc.Metadata["lang"].(string); ok && lang != "" {
				return lang
			}
		}
		if v != "code" && v != "doc" {
			return v
		}
	}
	source := strings.ToLower(doc.Source)
	switch {
	case strings.HasSuffix(source, ".go"):
		return "go"
	case strings.HasSuffix(source, ".md"), strings.HasSuffix(source, ".markdown"):
		return "md"
	}
	return ""
}

// chunkGo parses a Go file and emits one chunk per top-level
// declaration. If parsing fails, ok=false signals the caller to fall
// back to FixedWindow.
func (l *LangAware) chunkGo(doc Document) ([]Chunk, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, doc.Source, doc.Text, parser.ParseComments)
	if err != nil {
		return nil, false
	}
	out := make([]Chunk, 0, len(f.Decls))
	for _, decl := range f.Decls {
		startPos := fset.Position(decl.Pos())
		endPos := fset.Position(decl.End())
		// Include any leading line of doc comments by widening to the
		// start of the decl's line.
		start := startPos.Offset
		switch node := decl.(type) {
		case *ast.FuncDecl:
			if node.Doc != nil {
				start = fset.Position(node.Doc.Pos()).Offset
			}
		case *ast.GenDecl:
			if node.Doc != nil {
				start = fset.Position(node.Doc.Pos()).Offset
			}
		}
		end := endPos.Offset
		if start < 0 || end > len(doc.Text) || end <= start {
			continue
		}
		text := doc.Text[start:end]
		out = append(out, newChunk(doc, start, end, text))
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// chunkMarkdown splits on ATX-style headings (#, ##, ###). Each
// section becomes one chunk; preamble before the first heading is
// emitted as its own chunk.
func (l *LangAware) chunkMarkdown(doc Document) ([]Chunk, bool) {
	lines := strings.SplitAfter(doc.Text, "\n")
	if len(lines) == 0 {
		return nil, false
	}
	var (
		out     []Chunk
		section strings.Builder
		start   int
		pos     int
	)
	flush := func(endOffset int) {
		text := section.String()
		section.Reset()
		if strings.TrimSpace(text) == "" {
			return
		}
		out = append(out, newChunk(doc, start, endOffset, text))
	}
	var fence byte
	var fenceLen int
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		insideFence := fence != 0
		isFence := false
		if len(line)-len(trimmed) <= 3 && len(trimmed) >= 3 && (trimmed[0] == '`' || trimmed[0] == '~') {
			marker := trimmed[0]
			count := 0
			for count < len(trimmed) && trimmed[count] == marker {
				count++
			}
			if count >= 3 {
				if fence == 0 {
					fence = marker
					fenceLen = count
					isFence = true
				} else if marker == fence && count >= fenceLen && strings.TrimSpace(trimmed[count:]) == "" {
					fence = 0
					isFence = true
				}
			}
		}
		isHeading := !insideFence && !isFence && (strings.HasPrefix(line, "# ") ||
			strings.HasPrefix(line, "## ") ||
			strings.HasPrefix(line, "### "))
		if isHeading && section.Len() > 0 {
			flush(pos)
			start = pos
		}
		section.WriteString(line)
		pos += len(line)
	}
	flush(pos)
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// flatten sub-chunks any structural chunk longer than MaxRunes via the
// fallback chunker so we never ship a chunk that overflows the
// embedder's context.
func (l *LangAware) flatten(in []Chunk) ([]Chunk, error) {
	out := make([]Chunk, 0, len(in))
	for _, c := range in {
		if len([]rune(c.Text)) <= l.MaxRunes {
			out = append(out, c)
			continue
		}
		sub, err := chunkSafely(l.Fallback, Document{
			ID:       c.DocID,
			Source:   c.Source,
			Text:     c.Text,
			Metadata: c.Metadata,
		})
		if err != nil {
			return nil, err
		}
		// No chunks back means the fallback chose to drop the section.
		if sub, err = l.bound(sub); err != nil {
			return nil, err
		}
		// Re-base sub-chunk offsets against the parent chunk's start.
		for i := range sub {
			sub[i].Offset[0] += c.Offset[0]
			sub[i].Offset[1] += c.Offset[0]
			sub[i].ID = chunkID(sub[i].DocID, sub[i].Offset[0], sub[i].Offset[1])
		}
		out = append(out, sub...)
	}
	return out, nil
}
