package main

import (
	"strings"
	"testing"
)

func TestDocsCommandListsGrepsAndReadsRiskRegister(t *testing.T) {
	list := captureStdout(t, func() {
		runDocs([]string{"--list"})
	})
	if !strings.Contains(list, "project-architecture-review") {
		t.Fatalf("docs list missing risk-register topic:\n%s", list)
	}

	grep := captureStdout(t, func() {
		runDocs([]string{"--grep", "risk register"})
	})
	if !strings.Contains(grep, "project-architecture-review") {
		t.Fatalf("docs grep did not find risk register:\n%s", grep)
	}

	body := captureStdout(t, func() {
		runDocs([]string{"project-architecture-review"})
	})
	for _, want := range []string{
		"# GoFastr Current Risk Register",
		"old review findings",
		"Performance Witnesses",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("risk-register doc missing %q:\n%s", want, body)
		}
	}
}
