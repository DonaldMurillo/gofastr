package sitesearch

import (
	"strings"
	"testing"
)

func TestRepeatedBodyTermIsCapped(t *testing.T) {
	idx := Build([]Page{
		{URL: "/a", Title: "Alpha", Sections: []Section{{Text: strings.Repeat("cache ", 10)}}},
		{URL: "/h", Title: "Hotel", Sections: []Section{{}, {ID: "c", Heading: "cache", Text: "x"}}},
	})
	if got := urls(idx.Search("cache", 10)); got != "/h /a" {
		t.Fatalf("cache ranked %q, want /h /a: ten body repeats must not outweigh one heading", got)
	}
}
