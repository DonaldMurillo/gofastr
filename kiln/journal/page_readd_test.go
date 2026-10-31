package journal_test

import (
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
)

// add_page always starts a page at version 1, so a patch read from /p
// before a delete and re-add matched the new page's version and replaced
// it (2026-10-27 K20). Each add_page mints a generation, and a patch
// built from another generation is a conflict.
func TestPatchAfterReaddIsConflict(t *testing.T) {
	old, readded := pageAt(1, "x"), pageAt(1, "new")
	old.Gen, readded.Gen = "g1", "g2"
	patched := pageAt(2, "stale")
	patched.Gen = "g1"
	entries := []journal.Entry{
		pageEntry(t, "1", journal.OpAddPage, journal.AddPagePayload{Page: old}),
		planEntry(t, "p", journal.KindPlanProposed, journal.PlanProposedPayload{PlanID: "p1", Steps: []string{"delete"},
			Targets: []journal.PlanTarget{{Op: "delete_page", Name: "/p"}}}),
		planEntry(t, "q", journal.KindPlanApproved, journal.PlanApprovedPayload{PlanID: "p1"}),
		pageEntry(t, "2", journal.OpDeletePage, journal.DeletePagePayload{Path: "/p", Prev: old, PlanID: "p1"}),
		pageEntry(t, "3", journal.OpAddPage, journal.AddPagePayload{Page: readded}),
		pageEntry(t, "4", journal.OpUpdatePageElement, journal.UpdatePageElementPayload{Path: "/p", Prev: old, New: patched}),
	}
	s := journal.NewSession()
	var err error
	for _, e := range entries {
		if err = journal.Apply(s, e); err != nil {
			break
		}
	}
	pc, ok := errors.AsType[*journal.PageConflictError](err)
	if !ok || !pc.Replaced {
		t.Fatalf("err = %v, want a *PageConflictError for a replaced page", err)
	}
	if got := s.World.Pages["/p"].Tree.Children[0].Props["text"]; got != "new" {
		t.Fatalf("text = %v, want the re-added page kept", got)
	}
	// A recorded log loads as it ran, like a recorded version race.
	replayed, err := journal.ReplayEntries(entries)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if got := replayed.World.Pages["/p"].Tree.Children[0].Props["text"]; got != "stale" {
		t.Fatalf("replayed text = %v, want the recorded patch applied", got)
	}
}
