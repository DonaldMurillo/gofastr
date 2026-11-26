package semantic

import "testing"

func TestFixedWindowNegativeOverlapDoesNotSkipText(t *testing.T) {
	window := &FixedWindow{Size: 3, Overlap: -1}
	chunks, err := window.Chunk(Document{ID: "example", Text: "abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 || chunks[0].Text != "abc" || chunks[1].Text != "def" {
		t.Fatalf("got %+v, want contiguous chunks abc and def", chunks)
	}
}
