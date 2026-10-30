package docs

import "testing"

// A `#` line inside a fenced block is code (a shell or YAML comment),
// not a heading: it neither replaces the hit's heading nor counts toward
// the heading tier, and a matching one is a hit like any body line.
func TestScanTopicSkipsFencedHashLines(t *testing.T) {
	body := "# Title\n\n## Setup\n\n```yaml\n# needle comment\nkey: needle\n```\n\n" +
		"~~~sh\n# needle again\n~~~\n\nneedle after\n"
	score, hits := scanTopic("t", []byte(body), "needle")
	if score.headings != 0 {
		t.Errorf("fenced comments counted as %d heading matches", score.headings)
	}
	wantLines := []int{6, 7, 11, 14}
	if len(hits) != len(wantLines) {
		t.Fatalf("got %d hits, want %d: %+v", len(hits), len(wantLines), hits)
	}
	for i, h := range hits {
		if h.Line != wantLines[i] || h.Heading != "Setup" {
			t.Errorf("hit %d = line %d under %q, want line %d under \"Setup\"", i, h.Line, h.Heading, wantLines[i])
		}
	}
}

// The title is the first H1 outside a fence.
func TestExtractTitleSkipsFences(t *testing.T) {
	body := "```sh\n# not a title\n```\n# Real title\n"
	if got := extractTitle([]byte(body), "fallback"); got != "Real title" {
		t.Errorf("extractTitle = %q, want \"Real title\"", got)
	}
}

// A term too short to search is the caller's error, not zero hits: an
// agent must be able to tell "too short" from "not found".
func TestSearchShortTermIsAnError(t *testing.T) {
	for _, term := range []string{"", "a", "ab"} {
		hits, err := Search(term)
		if err == nil {
			t.Errorf("Search(%q) returned no error (%d hits)", term, len(hits))
		}
	}
	if _, err := Search("abc"); err != nil {
		t.Errorf("a three-character term was refused: %v", err)
	}
}
