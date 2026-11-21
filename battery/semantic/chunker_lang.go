package semantic

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"unicode/utf8"
)

// LangAware splits Go source on top-level declarations and Markdown on
// headings, falling back to a FixedWindow.
type LangAware struct {
	// MaxRunes caps one chunk in runes; a longer one is re-chunked by
	// Fallback. Defaults to 1024.
	MaxRunes int
	// Fallback chunks unknown kinds and over-large chunks. Defaults to
	// NewFixedWindow(512, 64).
	Fallback Chunker
}

// NewLangAware returns a LangAware chunker with sensible defaults.
func NewLangAware() *LangAware {
	return &LangAware{
		MaxRunes: 1024,
		Fallback: NewFixedWindow(512, 64),
	}
}

// Chunk implements Chunker.
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
		rebaseChunks(parts, c.Offset[0])
		out = append(out, parts...)
	}
	return out, nil
}

func rebaseChunks(parts []Chunk, delta int) {
	for i := range parts {
		part := &parts[i]
		part.Offset[0] += delta
		part.Offset[1] += delta
		part.ID = chunkID(part.DocID, part.Offset[0], part.Offset[1])
	}
}

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
		if sub, err = l.bound(sub); err != nil {
			return nil, err
		}
		rebaseChunks(sub, c.Offset[0])
		out = append(out, sub...)
	}
	return out, nil
}
