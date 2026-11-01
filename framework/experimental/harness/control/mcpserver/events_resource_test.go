package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/resources"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

func readEvents(id int, uri string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "resources/read",
		"params": map[string]any{"uri": uri},
	}
}

// eventsText pulls the envelopes out of a resources/read response.
func eventsText(t *testing.T, m map[string]any) []control.EventEnvelope {
	t.Helper()
	res, _ := m["result"].(map[string]any)
	contents, _ := res["contents"].([]any)
	if len(contents) != 1 {
		t.Fatalf("want one content entry, got %v", m)
	}
	text, _ := contents[0].(map[string]any)["text"].(string)
	var out []control.EventEnvelope
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("events text is not an envelope list: %v (%q)", err, text)
	}
	return out
}

// Reading the events resource with ?since=N replays what came after N.
func TestEventsResourceReplaysSince(t *testing.T) {
	send, recv, sess, bus := pipeServer(t)
	for _, txt := range []string{"one", "two", "three"} {
		if _, err := bus.Publish(control.TextDelta{Text: txt}, ""); err != nil {
			t.Fatal(err)
		}
	}
	send(readEvents(1, "harness/v1://session/"+string(sess)+"/events?since=1"))
	m := recv()
	if m == nil {
		t.Fatal("no response")
	}
	got := eventsText(t, m)
	if len(got) != 2 || got[0].ID != 2 || got[1].ID != 3 {
		t.Fatalf("want ids 2,3, got %+v", got)
	}

	send(readEvents(2, "harness/v1://session/"+string(sess)+"/events?since=x"))
	if m := recv(); m == nil || m["error"] == nil {
		t.Fatalf("malformed since must be refused, got %v", m)
	}
}

// A token scoped to another session cannot read this one's events.
func TestEventsResourceHonoursScope(t *testing.T) {
	sess := ids.NewSessionID()
	bus := engine.NewBus(sess)
	defer bus.Close()
	reg := tool.NewRegistry()
	mux := multiplex.New()
	mux.RegisterEngine(engine.NewEngine(sess, bus, fakeProvider{}, "fake", engine.NewDispatcher(bus, reg)))
	if _, err := bus.Publish(control.TextDelta{Text: "secret"}, ""); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	s := New(mux, resources.NewCatalog()).WithIO(strings.NewReader(""), &out)
	s.Claims = &auth.Claims{Sessions: []ids.SessionID{ids.NewSessionID()}}
	params, _ := json.Marshal(map[string]any{"uri": "harness/v1://session/" + string(sess) + "/events?since=0"})
	s.route(context.Background(), rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "resources/read", Params: params})
	if strings.Contains(out.String(), "secret") || !strings.Contains(out.String(), `"error"`) {
		t.Fatalf("out-of-scope read not refused: %s", out.String())
	}
}

// Over POST /mcp the read replays too, and the bearer's scope holds.
func TestHTTPEventsResourceReplays(t *testing.T) {
	s, sess, mux := newTestServer(t)
	bus := mux.EngineFor(sess).Bus
	for _, txt := range []string{"one", "two", "three"} {
		if _, err := bus.Publish(control.TextDelta{Text: txt}, ""); err != nil {
			t.Fatal(err)
		}
	}
	secret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	enc := auth.NewEncoder(secret)
	srv := httptest.NewServer(NewHTTPHandler(s, enc, auth.NewRevocationList()))
	defer srv.Close()
	body, _ := json.Marshal(readEvents(1, "harness/v1://session/"+string(sess)+"/events?since=2"))

	tok := mintHTTPScopeToken(t, enc, func(c *auth.Claims) { c.Sessions = []ids.SessionID{sess} })
	code, raw := postRawMCP(t, srv, tok, string(body))
	var m map[string]any
	if code != 200 || json.Unmarshal([]byte(raw), &m) != nil {
		t.Fatalf("status %d body %s", code, raw)
	}
	if got := eventsText(t, m); len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("want id 3, got %+v", got)
	}

	other := mintHTTPScopeToken(t, enc, func(c *auth.Claims) { c.Sessions = []ids.SessionID{ids.NewSessionID()} })
	if code, raw := postRawMCP(t, srv, other, string(body)); !mcpRawRefused(code, raw) || strings.Contains(raw, "three") {
		t.Fatalf("out-of-scope bearer read events: %d %s", code, raw)
	}
}
