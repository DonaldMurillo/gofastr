package docs

import (
	"slices"
	"strings"
	"testing"
)

func TestSweepRelevanceTiers(t *testing.T) {
	for _, c := range []struct {
		name          string
		better, worse topicScore
	}{
		{"title beats a lede", topicScore{name: "z", title: true}, topicScore{name: "a", lede: 9, headings: 9, body: 9}},
		{"lede beats headings", topicScore{name: "z", lede: 1}, topicScore{name: "a", headings: 9, body: 9}},
		{"headings beat body", topicScore{name: "z", headings: 1}, topicScore{name: "a", body: 9}},
		{"more body wins", topicScore{name: "z", body: 2}, topicScore{name: "a", body: 1}},
		{"name breaks a tie", topicScore{name: "a", body: 1}, topicScore{name: "b", body: 1}},
	} {
		if !c.better.moreRelevant(c.worse) || c.worse.moreRelevant(c.better) {
			t.Errorf("%s: ranking is not strict", c.name)
		}
	}
}

func TestSweepListIsSortedByName(t *testing.T) {
	topics, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.IsSortedFunc(topics, func(a, b Topic) int { return strings.Compare(a.Name, b.Name) }) {
		t.Fatal("List is not sorted by Name")
	}
}

func TestSweepDefaultHitCapIsFifty(t *testing.T) {
	for _, limit := range []int{0, -1} {
		hits, err := SearchWithLimit("the", limit)
		if err != nil || len(hits) != 50 {
			t.Errorf("SearchWithLimit(the, %d) = %d hits, %v; want 50", limit, len(hits), err)
		}
	}
	if hits, _ := Search("the"); len(hits) != 50 {
		t.Errorf("Search(the) = %d hits, want 50", len(hits))
	}
}

func TestSweepGetRefusesMalformedNamesBeforeReading(t *testing.T) {
	for name, want := range map[string]string{
		"":            "name required",
		"overview.md": "invalid topic name",
		"sub/x":       "invalid topic name",
		`sub\x`:       "invalid topic name",
	} {
		if _, err := Get(name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Get(%q) = %v, want an error saying %q", name, err, want)
		}
	}
}
