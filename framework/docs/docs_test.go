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
const MinExpectedTopics = 64

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

// commonMistakesExempt lists the docs that deliberately ship WITHOUT a
// "## Common mistakes" closing section. Every other content/*.md is a
// guide and the gate below requires the callout. To add a new doc
// without one, add it here WITH a reason — silence is not an option.
var commonMistakesExempt = map[string]string{
	"README":   "docs index page — meta, not a guide",
	"overview": "the map of the docs — index, not a guide",
	"agent-notes": "append-only review log — the doc IS a list of " +
		"mistakes and lessons",
	"perf-results": "benchmark data artifact — measurements, not a " +
		"how-to surface",
	"project-architecture-review": "current risk register — findings " +
		"artifact, not a guide",
	"harness-architecture": "architecture contract — its Hard rules / " +
		"Non-goals / Threat model sections already enumerate the " +
		"failure modes",
}

// TestGuideDocsEndWithCommonMistakes pins the docs convention that
// README.md and overview.md advertise: every guide doc carries a
// "## Common mistakes" callout grounded in the actual API. Data and
// index artifacts are exempted explicitly above; a new doc without the
// section fails here until it is either given a real callout or
// deliberately exempted with a reason.
func TestGuideDocsEndWithCommonMistakes(t *testing.T) {
	topics, err := List()
	if err != nil {
		t.Fatal(err)
	}
	const heading = "## Common mistakes"
	for _, top := range topics {
		if _, ok := commonMistakesExempt[top.Name]; ok {
			continue
		}
		body, err := Get(top.Name)
		if err != nil {
			t.Errorf("Get(%q): %v", top.Name, err)
			continue
		}
		found := false
		for _, ln := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(strings.TrimSpace(ln), heading) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("content/%s.md lacks a %q section — add one or "+
				"exempt the doc (with a reason) in commonMistakesExempt",
				top.Name, heading)
		}
	}
	// Keep the exemption list honest: every entry must name a real doc.
	byName := make(map[string]bool, len(topics))
	for _, top := range topics {
		byName[top.Name] = true
	}
	for name := range commonMistakesExempt {
		if !byName[name] {
			t.Errorf("commonMistakesExempt names %q, which is not an "+
				"embedded doc — stale entry?", name)
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
