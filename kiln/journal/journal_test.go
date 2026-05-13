package journal_test

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// --- Memory journal ----------------------------------------------------

func TestMemoryAppendReadTruncate(t *testing.T) {
	j := journal.NewMemory()

	for i := 0; i < 3; i++ {
		e, err := journal.NewEntry("e"+itoa(i), time.Unix(int64(i), 0).UTC(), journal.KindChatUser, "", journal.ChatMessagePayload{Text: "m" + itoa(i)})
		if err != nil {
			t.Fatalf("new entry: %v", err)
		}
		off, err := j.Append(e)
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		if off != i+1 {
			t.Fatalf("offset = %d, want %d", off, i+1)
		}
	}

	got, err := j.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("read returned %d entries, want 3", len(got))
	}

	if err := j.TruncateAfter(2); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	got, _ = j.Read()
	if len(got) != 2 {
		t.Fatalf("after truncate, len = %d, want 2", len(got))
	}

	if err := j.TruncateAfter(0); err != nil {
		t.Fatalf("truncate to 0: %v", err)
	}
	n, _ := j.Len()
	if n != 0 {
		t.Fatalf("len after full truncate = %d, want 0", n)
	}
}

// --- JSONL journal -----------------------------------------------------

func TestJSONLAppendReadAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	j, err := journal.OpenJSONL(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < 3; i++ {
		e, _ := journal.NewEntry("e"+itoa(i), time.Unix(int64(i), 0).UTC(), journal.KindChatUser, "", journal.ChatMessagePayload{Text: "m" + itoa(i)})
		if _, err := j.Append(e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := j.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen and read.
	j2, err := journal.OpenJSONL(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer j2.Close()
	n, _ := j2.Len()
	if n != 3 {
		t.Fatalf("len after reopen = %d, want 3", n)
	}
	got, err := j2.Read()
	if err != nil {
		t.Fatalf("read after reopen: %v", err)
	}
	if len(got) != 3 || got[2].ID != "e2" {
		t.Fatalf("read after reopen returned wrong entries: %#v", got)
	}

	// Append more after reopen.
	e, _ := journal.NewEntry("e3", time.Unix(3, 0).UTC(), journal.KindChatUser, "", journal.ChatMessagePayload{Text: "m3"})
	if _, err := j2.Append(e); err != nil {
		t.Fatalf("append after reopen: %v", err)
	}
	got, _ = j2.Read()
	if len(got) != 4 {
		t.Fatalf("len after append-after-reopen = %d, want 4", len(got))
	}
}

func TestJSONLTruncateAfter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trunc.jsonl")
	j, err := journal.OpenJSONL(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer j.Close()

	for i := 0; i < 5; i++ {
		e, _ := journal.NewEntry("e"+itoa(i), time.Unix(int64(i), 0).UTC(), journal.KindChatUser, "", journal.ChatMessagePayload{Text: "m" + itoa(i)})
		if _, err := j.Append(e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	if err := j.TruncateAfter(2); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	n, _ := j.Len()
	if n != 2 {
		t.Fatalf("len = %d, want 2", n)
	}
	got, _ := j.Read()
	if len(got) != 2 || got[1].ID != "e1" {
		t.Fatalf("after truncate read = %#v", got)
	}

	// Truncating beyond length must error.
	if err := j.TruncateAfter(10); err == nil {
		t.Fatal("truncate beyond length should error")
	}
}

// --- Replay tests ------------------------------------------------------

func TestReplayBuildsSessionFromEntries(t *testing.T) {
	j := journal.NewMemory()

	must := func(e journal.Entry, err error) journal.Entry {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return e
	}

	timestamps := true
	posts := &world.Entity{
		Name: "posts",
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
		},
		Timestamps: &timestamps,
	}
	homePage := &world.Page{
		Path:  "/",
		Title: "Home",
		Type:  "page",
		Tree:  world.Node{Kind: "div"},
	}
	hook := &world.Hook{
		ID:     "h1",
		Entity: "posts",
		When:   "before_create",
		Action: world.Action{Kind: world.ActionAudit, Params: map[string]any{"channel": "audit"}},
	}

	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	steps := []journal.Entry{
		must(journal.NewEntry("1", t0, journal.KindWorldEdit, journal.OpSetAppConfig,
			journal.SetAppConfigPayload{Config: world.AppConfig{Name: "blog", JSONCase: "snake"}})),
		must(journal.NewEntry("2", t0.Add(time.Second), journal.KindChatUser, "", journal.ChatMessagePayload{Text: "build me a blog"})),
		must(journal.NewEntry("3", t0.Add(2*time.Second), journal.KindWorldEdit, journal.OpAddEntity,
			journal.AddEntityPayload{Entity: posts})),
		must(journal.NewEntry("4", t0.Add(3*time.Second), journal.KindWorldEdit, journal.OpAddField,
			journal.AddFieldPayload{Entity: "posts", Field: world.Field{Name: "body", Type: "text"}})),
		must(journal.NewEntry("5", t0.Add(4*time.Second), journal.KindWorldEdit, journal.OpAddPage,
			journal.AddPagePayload{Page: homePage})),
		must(journal.NewEntry("6", t0.Add(5*time.Second), journal.KindWorldEdit, journal.OpAddHook,
			journal.AddHookPayload{Hook: hook})),
		must(journal.NewEntry("7", t0.Add(6*time.Second), journal.KindToolCall, "",
			journal.ToolCallPayload{CallID: "tc1", Name: "add_entity", Args: map[string]any{"name": "posts"}})),
		must(journal.NewEntry("8", t0.Add(7*time.Second), journal.KindToolResult, "",
			journal.ToolResultPayload{CallID: "tc1", OK: true})),
		must(journal.NewEntry("9", t0.Add(8*time.Second), journal.KindPlanProposed, "",
			journal.PlanProposedPayload{PlanID: "p1", Steps: []string{"a", "b"}, Reason: "big change"})),
		must(journal.NewEntry("10", t0.Add(9*time.Second), journal.KindPlanApproved, "",
			journal.PlanApprovedPayload{PlanID: "p1"})),
	}
	for _, e := range steps {
		if _, err := j.Append(e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	s, err := journal.Replay(j)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}

	if s.World.App.Name != "blog" {
		t.Errorf("App.Name = %q, want blog", s.World.App.Name)
	}
	postsOut, ok := s.World.Entities["posts"]
	if !ok {
		t.Fatal("posts entity missing after replay")
	}
	if len(postsOut.Fields) != 2 || postsOut.Fields[1].Name != "body" {
		t.Errorf("posts.Fields = %#v, want [title body]", postsOut.Fields)
	}
	if _, ok := s.World.Pages["/"]; !ok {
		t.Error("/ page missing after replay")
	}
	if len(s.World.Hooks) != 1 || s.World.Hooks[0].ID != "h1" {
		t.Errorf("hooks = %#v", s.World.Hooks)
	}

	if len(s.Chat) != 3 {
		t.Errorf("Chat events = %d, want 3", len(s.Chat))
	}
	if s.Chat[0].Message == nil || s.Chat[0].Message.Text != "build me a blog" {
		t.Errorf("first chat event mismatch: %#v", s.Chat[0])
	}
	if s.Chat[1].Call == nil || s.Chat[1].Call.CallID != "tc1" {
		t.Errorf("tool call event mismatch: %#v", s.Chat[1])
	}
	if s.Chat[2].Result == nil || !s.Chat[2].Result.OK {
		t.Errorf("tool result event mismatch: %#v", s.Chat[2])
	}

	plan, ok := s.Plans["p1"]
	if !ok {
		t.Fatal("plan p1 missing")
	}
	if !plan.Approved {
		t.Error("plan p1 should be approved")
	}
}

func TestReplayEquivalenceMemoryAndJSONL(t *testing.T) {
	// Build a memory journal, persist its entries to JSONL, replay both,
	// expect equal sessions. This is the load-bearing invariant for the
	// freeze/undo/replay story.
	mem := journal.NewMemory()
	posts := &world.Entity{
		Name:   "posts",
		Fields: []world.Field{{Name: "title", Type: "string"}},
	}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, e := range []journal.Entry{
		mustEntry(t, "1", t0, journal.KindWorldEdit, journal.OpAddEntity, journal.AddEntityPayload{Entity: posts}),
		mustEntry(t, "2", t0.Add(time.Second), journal.KindWorldEdit, journal.OpAddField,
			journal.AddFieldPayload{Entity: "posts", Field: world.Field{Name: "body", Type: "text"}}),
		mustEntry(t, "3", t0.Add(2*time.Second), journal.KindChatUser, "", journal.ChatMessagePayload{Text: "go"}),
	} {
		if _, err := mem.Append(e); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	dir := t.TempDir()
	jpath := filepath.Join(dir, "j.jsonl")
	disk, err := journal.OpenJSONL(jpath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	memEntries, _ := mem.Read()
	for _, e := range memEntries {
		if _, err := disk.Append(e); err != nil {
			t.Fatalf("append to disk: %v", err)
		}
	}
	disk.Close()

	disk2, err := journal.OpenJSONL(jpath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer disk2.Close()

	memSess, err := journal.Replay(mem)
	if err != nil {
		t.Fatalf("replay mem: %v", err)
	}
	diskSess, err := journal.Replay(disk2)
	if err != nil {
		t.Fatalf("replay disk: %v", err)
	}
	if !reflect.DeepEqual(memSess.World, diskSess.World) {
		t.Fatalf("worlds differ after replay\nmem:  %#v\ndisk: %#v", memSess.World, diskSess.World)
	}
	if len(memSess.Chat) != len(diskSess.Chat) {
		t.Fatalf("chat lengths differ: %d vs %d", len(memSess.Chat), len(diskSess.Chat))
	}
}

func TestReplayRejectsConflictingOps(t *testing.T) {
	j := journal.NewMemory()
	t0 := time.Now().UTC()
	posts := &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	if _, err := j.Append(mustEntry(t, "1", t0, journal.KindWorldEdit, journal.OpAddEntity, journal.AddEntityPayload{Entity: posts})); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := j.Append(mustEntry(t, "2", t0.Add(time.Second), journal.KindWorldEdit, journal.OpAddEntity, journal.AddEntityPayload{Entity: posts})); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := journal.Replay(j); err == nil {
		t.Fatal("replay should error on duplicate add_entity")
	}
}

func TestReplayUndoViaTruncate(t *testing.T) {
	j := journal.NewMemory()
	t0 := time.Now().UTC()
	posts := &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	if _, err := j.Append(mustEntry(t, "1", t0, journal.KindWorldEdit, journal.OpAddEntity, journal.AddEntityPayload{Entity: posts})); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append(mustEntry(t, "2", t0.Add(time.Second), journal.KindWorldEdit, journal.OpAddField,
		journal.AddFieldPayload{Entity: "posts", Field: world.Field{Name: "body", Type: "text"}})); err != nil {
		t.Fatal(err)
	}

	full, _ := journal.Replay(j)
	if len(full.World.Entities["posts"].Fields) != 2 {
		t.Fatalf("expected 2 fields before undo, got %d", len(full.World.Entities["posts"].Fields))
	}

	if err := j.TruncateAfter(1); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	undone, _ := journal.Replay(j)
	if len(undone.World.Entities["posts"].Fields) != 1 {
		t.Fatalf("expected 1 field after undo, got %d", len(undone.World.Entities["posts"].Fields))
	}
}

func TestReplayUnknownOpFails(t *testing.T) {
	e, _ := journal.NewEntry("x", time.Now().UTC(), journal.KindWorldEdit, journal.Op("bogus"), nil)
	_, err := journal.ReplayEntries([]journal.Entry{e})
	if err == nil {
		t.Fatal("unknown op should error")
	}
}

// --- helpers -----------------------------------------------------------

func mustEntry(t *testing.T, id string, ts time.Time, kind journal.Kind, op journal.Op, payload any) journal.Entry {
	t.Helper()
	e, err := journal.NewEntry(id, ts, kind, op, payload)
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	return e
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	const digits = "0123456789"
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = digits[i%10]
		i /= 10
	}
	return string(buf[pos:])
}
