package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestPresenceFanoutDocs(t *testing.T) {
	page := serve(t, http.MethodGet, "/examples/presence")
	if page.Code != http.StatusOK {
		t.Fatalf("presence page status = %d", page.Code)
	}
	text := page.Body.String()
	if strings.Contains(text, "future work") || !strings.Contains(text, "WithFanout") || !strings.Contains(text, "no fanout") {
		t.Fatal("presence demo must distinguish its local roster from available WithFanout aggregation")
	}
	for _, intent := range docIntents {
		for _, entry := range intent.Docs {
			if entry.Slug == "presence" {
				if strings.Contains(entry.Desc, "single-replica") || !strings.Contains(entry.Desc, "WithFanout") {
					t.Fatalf("presence catalog omits cross-replica support: %q", entry.Desc)
				}
				return
			}
		}
	}
	t.Fatal("presence catalog entry is missing")
}
