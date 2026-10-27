package journal_test

import (
	"errors"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func pageAt(version int, text string) *world.Page {
	return &world.Page{Path: "/p", Version: version, Tree: world.Node{ID: "root", Kind: "div", Children: []world.Node{
		{ID: "a", Kind: "paragraph", Props: map[string]any{"text": text}},
	}}}
}

func pageEntry(t *testing.T, id string, op journal.Op, payload any) journal.Entry {
	t.Helper()
	e, err := journal.NewEntry(id, time.Unix(0, 0).UTC(), journal.KindWorldEdit, op, payload)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// Two patches built from version 1: the second must be refused as a
// typed conflict, not swapped in over the first.
func TestStalePagePatchIsConflict(t *testing.T) {
	entries := racedPatches(t)
	s := journal.NewSession()
	var err error
	for _, e := range entries {
		if err = journal.Apply(s, e); err != nil {
			break
		}
	}
	pc, ok := errors.AsType[*journal.PageConflictError](err)
	if !ok {
		t.Fatalf("err = %v, want *PageConflictError", err)
	}
	if pc.Have != 2 || pc.Want != 1 {
		t.Fatalf("conflict = %+v, want have 2 want 1", pc)
	}
}

// A log written before the check may already hold two patches from one
// version. It still loads, as it ran: the later patch wins.
func TestRecordedPatchRaceReplays(t *testing.T) {
	s, err := journal.ReplayEntries(racedPatches(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.World.Pages["/p"].Tree.Children[0].Props["text"]; got != "second" {
		t.Fatalf("text = %v, want second", got)
	}
}

func racedPatches(t *testing.T) []journal.Entry {
	return []journal.Entry{
		pageEntry(t, "1", journal.OpAddPage, journal.AddPagePayload{Page: pageAt(1, "x")}),
		pageEntry(t, "2", journal.OpUpdatePageElement, journal.UpdatePageElementPayload{Path: "/p", Prev: pageAt(1, "x"), New: pageAt(2, "first")}),
		pageEntry(t, "3", journal.OpUpdatePageElement, journal.UpdatePageElementPayload{Path: "/p", Prev: pageAt(1, "x"), New: pageAt(2, "second")}),
	}
}

// Replay refuses what the rebuild cannot register: two routes the mux
// cannot hold together, and an entity whose CRUD mount is a page's path.
func TestReplayRefusesMuxCollisions(t *testing.T) {
	posts := &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	cases := map[string][]journal.Entry{
		"overlapping routes": {
			pageEntry(t, "1", journal.OpAddRoute, journal.AddRoutePayload{Route: &world.Route{Method: "GET", Path: "/x/{id}"}}),
			pageEntry(t, "2", journal.OpAddRoute, journal.AddRoutePayload{Route: &world.Route{Method: "GET", Path: "/{name}/y"}}),
		},
		"entity onto page": {
			pageEntry(t, "1", journal.OpAddPage, journal.AddPagePayload{Page: &world.Page{Path: "/api/posts", Tree: world.Node{Kind: "div"}}}),
			pageEntry(t, "2", journal.OpAddEntity, journal.AddEntityPayload{Entity: posts}),
		},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := journal.ReplayEntries(entries); err == nil {
				t.Fatal("replay accepted an edit the rebuild cannot register")
			}
		})
	}
}

// delete_entity takes the entity's seeds with it; another entity's stay.
func TestDeleteEntityDropsItsSeeds(t *testing.T) {
	ent := func(n string) *world.Entity {
		return &world.Entity{Name: n, Fields: []world.Field{{Name: "title", Type: "string"}}}
	}
	seed := func(n string) *world.Seed { return &world.Seed{Entity: n, Rows: []map[string]any{{"title": "x"}}} }
	s, err := journal.ReplayEntries([]journal.Entry{
		pageEntry(t, "1", journal.OpAddEntity, journal.AddEntityPayload{Entity: ent("posts")}),
		pageEntry(t, "2", journal.OpAddEntity, journal.AddEntityPayload{Entity: ent("tags")}),
		pageEntry(t, "3", journal.OpAddSeed, journal.AddSeedPayload{Seed: seed("posts")}),
		pageEntry(t, "4", journal.OpAddSeed, journal.AddSeedPayload{Seed: seed("tags")}),
		planEntry(t, "5", journal.KindPlanProposed, journal.PlanProposedPayload{PlanID: "p", Steps: []string{"x"},
			Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}}}),
		planEntry(t, "6", journal.KindPlanApproved, journal.PlanApprovedPayload{PlanID: "p"}),
		pageEntry(t, "7", journal.OpDeleteEntity, journal.DeleteEntityPayload{Name: "posts", PlanID: "p"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.World.Seeds) != 1 || s.World.Seeds[0].Entity != "tags" {
		t.Fatalf("seeds = %+v, want only tags", s.World.Seeds)
	}
}

func planEntry(t *testing.T, id string, kind journal.Kind, payload any) journal.Entry {
	t.Helper()
	e, err := journal.NewEntry(id, time.Unix(0, 0).UTC(), kind, "", payload)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// A sequential log replays: each patch's Prev is the page it follows,
// including a page journaled before versions existed (version 0) and a
// hand-authored entry with no prev.
func TestSequentialPagePatchesReplay(t *testing.T) {
	s, err := journal.ReplayEntries([]journal.Entry{
		pageEntry(t, "1", journal.OpAddPage, journal.AddPagePayload{Page: pageAt(0, "x")}),
		pageEntry(t, "2", journal.OpUpdatePageElement, journal.UpdatePageElementPayload{Path: "/p", Prev: pageAt(0, "x"), New: pageAt(1, "a")}),
		pageEntry(t, "3", journal.OpUpdatePageElement, journal.UpdatePageElementPayload{Path: "/p", Prev: pageAt(1, "a"), New: pageAt(2, "b")}),
		pageEntry(t, "4", journal.OpUpdatePageElement, journal.UpdatePageElementPayload{Path: "/p", New: pageAt(3, "c")}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.World.Pages["/p"].Version; got != 3 {
		t.Fatalf("version = %d, want 3", got)
	}
}
