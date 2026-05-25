package docs

import (
	"strings"
	"testing"
)

// MinExpectedTopics is the manifest floor: if a doc is accidentally
// deleted in a rebase, the embed FS is silently smaller and the count
// regresses. Bumping this constant in the same commit that adds a new
// doc enforces "every doc removal requires an explicit decision."
//
// Bump it up when adding docs. Be reluctant to decrement.
const MinExpectedTopics = 40

func TestEmbedManifestFloor(t *testing.T) {
	topics, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(topics) < MinExpectedTopics {
		t.Errorf("docs count regressed: %d < %d (was something accidentally deleted?)",
			len(topics), MinExpectedTopics)
	}
}

func TestListNonEmpty(t *testing.T) {
	topics, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(topics) == 0 {
		t.Fatal("List() returned 0 topics — embed broken?")
	}
	for _, top := range topics {
		if top.Name == "" {
			t.Errorf("topic name empty: %+v", top)
		}
		if top.Title == "" {
			t.Errorf("topic %q: empty title", top.Name)
		}
	}
}

func TestGetKnownTopic(t *testing.T) {
	topics, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(topics) == 0 {
		t.Skip("no topics in embed")
	}
	body, err := Get(topics[0].Name)
	if err != nil {
		t.Fatalf("Get(%q): %v", topics[0].Name, err)
	}
	if len(body) == 0 {
		t.Error("Get returned empty body")
	}
}

func TestGetUnknownTopic(t *testing.T) {
	if _, err := Get("definitely-not-a-real-topic-xyzzy"); err == nil {
		t.Error("Get should error for unknown topic")
	}
}

func TestGetRejectsPathTraversal(t *testing.T) {
	for _, bad := range []string{
		"../../../etc/passwd",
		"some/sub/path",
		"with.dot",
		`win\\path`,
	} {
		if _, err := Get(bad); err == nil {
			t.Errorf("Get(%q) should error", bad)
		}
	}
}

func TestSearchFindsKnownTerm(t *testing.T) {
	// "entity" should appear in many docs (entity-declarations, hooks…).
	hits, err := Search("entity")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Error("Search(\"entity\") returned 0 hits")
	}
	for _, h := range hits {
		if !strings.Contains(strings.ToLower(h.Excerpt), "entity") {
			t.Errorf("hit excerpt missing term: %+v", h)
		}
	}
}

func TestSearchEmptyTerm(t *testing.T) {
	hits, _ := Search("")
	if len(hits) != 0 {
		t.Errorf("empty term should return no hits, got %d", len(hits))
	}
}

// TestSearchRejectsShortTerm pins the DoS-mitigation: queries shorter
// than 3 chars match noise (e.g. "a", "of") and would return thousands
// of hits across the corpus. The function should return zero hits for
// short terms without scanning the corpus.
func TestSearchRejectsShortTerm(t *testing.T) {
	hits, _ := Search("a")
	if len(hits) != 0 {
		t.Errorf("short term should return zero hits to bound the response, got %d", len(hits))
	}
}

// TestSearchWithLimit pins the operator-facing cap: when a caller
// explicitly asks for at most N hits, the function returns no more
// than N — capping unbounded responses for clients with strict
// payload budgets (MCP, narrow context).
func TestSearchWithLimit(t *testing.T) {
	hits, _ := SearchWithLimit("entity", 5)
	if len(hits) > 5 {
		t.Errorf("SearchWithLimit returned %d hits, want <= 5", len(hits))
	}
}
