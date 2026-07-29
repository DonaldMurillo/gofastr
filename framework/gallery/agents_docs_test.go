package gallery

import (
	"os"
	"strings"
	"testing"
)

// agents.md is the agent-facing reference for this package. It once
// advertised gallery.NoteOnlySlugs, which is not exported — only the
// private noteOnlySlugs map and the IsNoteOnly accessor exist. The bullet
// above it had the identical defect (CodeSnippets["button"]) and was fixed
// without catching this one, so the claim is pinned.
func TestAgentsMDHasNoPhantomSymbols(t *testing.T) {
	b, err := os.ReadFile("agents.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "NoteOnlySlugs") {
		t.Error("framework/gallery/agents.md advertises `NoteOnlySlugs`, which is not " +
			"exported; catalog.go declares the private noteOnlySlugs and IsNoteOnly " +
			"is the only accessor")
	}
}

// Every accessor agents.md names in its example block must resolve to a real
// symbol with the documented behaviour. A previous pass fixed a non-existent
// CodeSnippets["button"] reference one line above a NoteOnlySlugs reference;
// this gates the whole example so neither defect class recurs.
func TestAgentsMDAdvertisedAccessorsExist(t *testing.T) {
	if got := CodeSnippet("modal"); got == "" {
		t.Errorf(`agents.md advertises CodeSnippet("modal") as example Go source, got ""`)
	}
	if got := PkgForSlug("button"); got != "framework/ui" {
		t.Errorf(`agents.md says PkgForSlug("button") == "framework/ui", got %q`, got)
	}
	if !IsNoteOnly("datatable") {
		t.Error(`agents.md says IsNoteOnly("datatable") is true`)
	}
	if got := CategorySlug("Buttons & links"); got != "buttons-links" {
		t.Errorf(`agents.md says CategorySlug("Buttons & links") == "buttons-links", got %q`, got)
	}
}
