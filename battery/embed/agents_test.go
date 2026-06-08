package embed_test

import (
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/battery/embed"
	"github.com/DonaldMurillo/gofastr/framework/agentsinv"
)

// TestEmbedAgentsInit verifies that importing battery/embed registers an
// agentsinv entry with the correct name, kind, import path, and non-empty
// markdown.  The init() in agents.go fires at package-import time; we read
// the inventory before any Reset so the side-effect is observable.
func TestEmbedAgentsInit(t *testing.T) {
	all := agentsinv.All()
	var found *agentsinv.Entry
	for i := range all {
		if all[i].Name == "embed" {
			found = &all[i]
			break
		}
	}
	if found == nil {
		t.Fatal("agentsinv.All() missing 'embed' entry — agents.go not wired")
	}
	if found.Kind != agentsinv.KindBattery {
		t.Errorf("Kind = %q, want KindBattery", found.Kind)
	}
	if found.ImportPath != "github.com/DonaldMurillo/gofastr/battery/embed" {
		t.Errorf("ImportPath = %q", found.ImportPath)
	}
	if !strings.Contains(found.Markdown, "battery/embed") {
		t.Errorf("Markdown missing 'battery/embed'; got:\n%s", found.Markdown)
	}
	if !strings.Contains(found.Markdown, "semantic") {
		t.Errorf("Markdown missing keyword 'semantic'; agents.md should describe the battery")
	}
}
