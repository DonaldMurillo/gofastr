package semantic

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"maps"
	"strings"
	"unicode/utf8"
)

// FixedWindow splits text into fixed-size rune windows with overlap.
type FixedWindow struct {
	Size    int // window size, in runes
	Overlap int // overlap between consecutive windows, in runes
}

// NewFixedWindow returns a FixedWindow, clamping overlap to [0, size-1].
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
	if !utf8.ValidString(doc.Text) {
		return nil, fmt.Errorf("semantic: document text must be valid UTF-8")
	}
	if f.Size <= 0 {
		return nil, fmt.Errorf("semantic: window size must be positive")
	}
	text := doc.Text
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	runes := []rune(text)
	if len(runes) <= f.Size {
		return []Chunk{newChunk(doc, 0, len(text), text)}, nil
	}
	step := f.Size - min(max(f.Overlap, 0), f.Size-1)
	var chunks []Chunk
	prevStart := 0
	byteStart := 0
	for start := 0; start < len(runes); start += step {
		if start > prevStart {
			byteStart += len(string(runes[prevStart:start]))
			prevStart = start
		}
		end := min(start+f.Size, len(runes))
		chunkText := string(runes[start:end])
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
	var md map[string]any
	if len(doc.Metadata) > 0 {
		md = make(map[string]any, len(doc.Metadata))
		maps.Copy(md, doc.Metadata)
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
