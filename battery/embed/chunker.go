package embed

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
)

// FixedWindow is a language-agnostic chunker that splits text into
// fixed-size rune windows with overlap. It does not look at token
// boundaries — that's the embedder's concern — so chunks are always
// reproducible from byte offsets alone.
type FixedWindow struct {
	Size    int // window size, in runes
	Overlap int // overlap between consecutive windows, in runes
}

// NewFixedWindow validates size/overlap and returns a chunker. Overlap
// is clamped to [0, size-1].
func NewFixedWindow(size, overlap int) *FixedWindow {
	if size <= 0 {
		size = 512
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= size {
		overlap = size - 1
	}
	return &FixedWindow{Size: size, Overlap: overlap}
}

// Chunk implements [Chunker].
func (f *FixedWindow) Chunk(doc Document) ([]Chunk, error) {
	text := doc.Text
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	runes := []rune(text)
	if len(runes) <= f.Size {
		return []Chunk{newChunk(doc, 0, len(text), text)}, nil
	}
	step := f.Size - f.Overlap
	if step <= 0 {
		step = 1
	}
	var chunks []Chunk
	for start := 0; start < len(runes); start += step {
		end := start + f.Size
		if end > len(runes) {
			end = len(runes)
		}
		chunkText := string(runes[start:end])
		// Convert rune offsets to byte offsets so callers can map back
		// into the original source file.
		byteStart := len(string(runes[:start]))
		byteEnd := byteStart + len(chunkText)
		chunks = append(chunks, newChunk(doc, byteStart, byteEnd, chunkText))
		if end == len(runes) {
			break
		}
	}
	return chunks, nil
}

func newChunk(doc Document, byteStart, byteEnd int, text string) Chunk {
	id := chunkID(doc.ID, byteStart, byteEnd)
	// Shallow-copy metadata so callers can mutate doc.Metadata without
	// affecting stored chunks.
	var md map[string]any
	if len(doc.Metadata) > 0 {
		md = make(map[string]any, len(doc.Metadata))
		for k, v := range doc.Metadata {
			md[k] = v
		}
	}
	return Chunk{
		ID:       id,
		DocID:    doc.ID,
		Source:   doc.Source,
		Text:     text,
		Offset:   [2]int{byteStart, byteEnd},
		Metadata: md,
	}
}

func chunkID(docID string, start, end int) string {
	h := sha1.Sum([]byte(fmt.Sprintf("%s:%d:%d", docID, start, end)))
	return hex.EncodeToString(h[:8])
}
