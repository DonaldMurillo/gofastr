package docs

import "testing"

// TestSearchRanksRelevantTopicsFirst pins the --grep ranking: the topic
// a term is ABOUT comes first, not whichever topic is alphabetically
// early. Before the ranking, `gofastr docs --grep sidebar` listed
// topics alphabetically, so auth, blueprints and harness-architecture
// came before layouts — the teaching topic was buried under topics
// that merely mention the word. Ranking tiers, strongest first: a
// title match, a match in the topic's own lede, a section-heading
// match, then body-mention count; the topic name breaks ties.
func TestSearchRanksRelevantTopicsFirst(t *testing.T) {
	for _, term := range []string{
		"sidebar",
		"outlet",
		"breadcrumbs",
		"table of contents",
		"header and footer",
	} {
		hits, err := Search(term)
		if err != nil {
			t.Fatalf("Search(%q): %v", term, err)
		}
		if len(hits) == 0 {
			t.Fatalf("Search(%q): no hits — the corpus lost the topic", term)
		}
		first := hits[0].Topic
		if first != "layouts" && first != "ui-composition-recipes" {
			t.Errorf("Search(%q) ranks %q first; the layout/recipe topic that teaches the subject must lead, got hits: %s, %s, %s",
				term, first, hits[0].Topic, topicAt(hits, 1), topicAt(hits, 2))
		}
	}
}

// topicAt names the topic of the n-th hit's first distinct-topic run,
// or "?" past the end — context for the failure message above.
func topicAt(hits []SearchHit, n int) string {
	seen := 0
	last := ""
	for _, h := range hits {
		if h.Topic != last {
			seen++
			last = h.Topic
		}
		if seen == n+1 {
			return h.Topic
		}
	}
	return "?"
}

// TestSearchKeepsLineOrderWithinTopic: relevance reorders TOPICS, never
// the lines inside one — a reader scanning a topic's hits reads them
// top to bottom.
func TestSearchKeepsLineOrderWithinTopic(t *testing.T) {
	hits, err := Search("outlet")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	lastTopic, lastLine := "", 0
	for _, h := range hits {
		if h.Topic == lastTopic {
			if h.Line <= lastLine {
				t.Fatalf("hits out of line order inside %q: L%d after L%d", h.Topic, h.Line, lastLine)
			}
		}
		lastTopic, lastLine = h.Topic, h.Line
	}
}
