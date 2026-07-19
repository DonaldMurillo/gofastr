package main

import (
	"strings"
	"testing"
)

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

	for _, term := range []string{"optimistic", "realtime", "live dashboard", "reactive state", "rollback", "reconciliation"} {
		result := captureStdout(t, func() { runDocs([]string{"--grep", term}) })
		if !strings.Contains(result, "ui-capability-map") {
			t.Fatalf("docs grep %q did not route to ui-capability-map:\n%s", term, result)
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
