package main

import (
	"strings"
	"testing"
)

// firstGrepTopic returns the first topic header line ("─ name") of a
// --grep output, "" when none.
func firstGrepTopic(out string) string {
	for _, ln := range strings.Split(out, "\n") {
		if after, ok := strings.CutPrefix(ln, "─ "); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

func TestDocsCommandListsGrepsAndReads(t *testing.T) {
	list := captureStdout(t, func() {
		runDocs([]string{"--list"})
	})
	if !strings.Contains(list, "entity-declarations") {
		t.Fatalf("docs list missing entity-declarations topic:\n%s", list)
	}
	if !strings.Contains(list, "ui-capability-map") {
		t.Fatalf("docs list missing task-oriented UI capability map:\n%s", list)
	}
	grep := captureStdout(t, func() {
		runDocs([]string{"--grep", "owner scoping"})
	})
	if !strings.Contains(grep, "entity-declarations") {
		t.Fatalf("docs grep did not find owner scoping:\n%s", grep)
	}

	// Relevance routing (ranked by title, lede, headings, then body
	// count): each term leads with the topic that owns it — the one
	// NAMED after the concept when there is one, the capability map for
	// the capability questions.
	for term, want := range map[string]string{
		"optimistic":     "optimistic-ui",     // titled "Optimistic UI"
		"realtime":       "ui-capability-map", // the capability ladder
		"live dashboard": "live-dashboards",   // titled "Live dashboards"
		"reactive state": "ui-capability-map",
		"rollback":       "optimistic-ui", // the rollback section
		"reconciliation": "optimistic-ui",
		"owner scoping":  "a2a", // the "Auth and owner scoping" section
	} {
		result := captureStdout(t, func() { runDocs([]string{"--grep", term}) })
		first := firstGrepTopic(result)
		if first != want {
			t.Fatalf("docs grep %q routed to %q, want %q:\n%s", term, first, want, result)
		}
	}

	capabilityMap := captureStdout(t, func() {
		runDocs([]string{"ui-capability-map"})
	})
	for _, want := range []string{"The state boundary", "Live dashboards", "Stateless and affinity-bound islands", "Common mistakes"} {
		if !strings.Contains(capabilityMap, want) {
			t.Fatalf("ui-capability-map doc missing %q:\n%s", want, capabilityMap)
		}
	}
	body := captureStdout(t, func() {
		runDocs([]string{"entity-declarations"})
	})
	for _, want := range []string{
		"EntityConfig",
		"Common mistakes",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("entity-declarations doc missing %q:\n%s", want, body)
		}
	}
}
