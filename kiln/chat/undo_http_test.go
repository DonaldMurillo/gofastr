package chat_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// toolPost drives POST /kiln/tool/<name> the way the agent's $KILN_URL
// client does, and decodes the Result.
func toolPost(t *testing.T, l *live.Live, name, body string) protocol.Result {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/kiln/tool/"+name, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /kiln/tool/%s: status %d body %s", name, rec.Code, rec.Body.String())
	}
	var res protocol.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("POST /kiln/tool/%s: decode %v: %s", name, err, rec.Body.String())
	}
	return res
}

// The HTTP tool API wraps every journaled tool in a tool_call and a
// tool_result envelope, so one add_entity writes three journal entries.
// Undo used to truncate exactly one: it removed the tool_result, left
// the entity in the world, and re-derived the DB anyway, so the edit
// stayed and the rows went. Undo reverts the most recent world edit.
func TestHTTPUndoRevertsEnvelopedEdit(t *testing.T) {
	l, _ := setup(t)
	if res := toolPost(t, l, "add_entity", `{"entity":{"name":"posts","fields":[{"name":"title","type":"string"}]}}`); !res.OK {
		t.Fatalf("add_entity: %+v", res)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/posts", strings.NewReader(`{"title":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code/100 != 2 {
		t.Fatalf("POST /api/posts: status %d body %s", rec.Code, rec.Body.String())
	}

	if res := toolPost(t, l, "undo", `{}`); !res.OK {
		t.Fatalf("undo: %+v", res)
	}
	l.ReadSession(func(sess *journal.Session) {
		if _, ok := sess.World.Entities["posts"]; ok {
			t.Errorf("undo left entity posts in the world: it truncated an envelope, not the edit")
		}
	})
	entries, err := l.Journal().Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("undo left journal entry kind=%s op=%s (want an empty journal)", e.Kind, e.Op)
	}
}
