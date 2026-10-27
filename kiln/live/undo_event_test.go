package live_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Undo rewrites the world a visitor's page renders from, so it must tell
// the bus: the reload client refreshes on the event. UndoLast sent
// nothing, so the page kept showing the edit undo had just reverted.
func TestUndoBroadcastsEvent(t *testing.T) {
	l, _ := newTestLive(t, journal.NewMemory())
	ch, unsub := l.Subscribe()
	defer unsub()

	page := &world.Page{Path: "/about", Tree: world.Node{Kind: "div"}}
	if err := l.Apply(newEntry(t, "e1", time.Now(), journal.KindWorldEdit, journal.OpAddPage,
		journal.AddPagePayload{Page: page})); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if ev := recvEvent(t, ch); ev.Kind != string(journal.KindWorldEdit) {
		t.Fatalf("first event kind = %q, want world_edit", ev.Kind)
	}
	if err := l.UndoLast(); err != nil {
		t.Fatalf("UndoLast: %v", err)
	}
	ev := recvEvent(t, ch)
	if ev.Kind != live.EventUndo {
		t.Errorf("event after undo kind = %q, want %q", ev.Kind, live.EventUndo)
	}
}

// The reload client refreshes on every world edit and on undo. It used to
// refresh only on add/delete page and route, so update_page_element on
// the page the visitor was looking at changed nothing on screen.
func TestReloadClientCoversEveryEdit(t *testing.T) {
	rec := httptest.NewRecorder()
	live.ServeReloadJS(rec, httptest.NewRequest("GET", "/.kiln/reload.js", nil))
	js := rec.Body.String()
	if strings.Contains(js, "RELOAD_OPS") {
		t.Error("reload client still gates world_edit refreshes on an op allow-list")
	}
	for _, ev := range []string{"'world_edit'", "'session_reset'", "'" + live.EventUndo + "'"} {
		if !strings.Contains(js, "addEventListener("+ev) {
			t.Errorf("reload client does not listen for %s", ev)
		}
	}
}

func recvEvent(t *testing.T, ch <-chan live.Event) live.Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(time.Second):
		t.Fatal("no event on the bus")
		return live.Event{}
	}
}
