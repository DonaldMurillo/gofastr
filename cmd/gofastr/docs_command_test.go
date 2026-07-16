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

	grep := captureStdout(t, func() {
		runDocs([]string{"--grep", "owner scoping"})
	})
	if !strings.Contains(grep, "entity-declarations") {
		t.Fatalf("docs grep did not find owner scoping:\n%s", grep)
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
