package a2a

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// postRaw posts one literal JSON-RPC body and parses the envelope, for
// shapes the map-based helpers cannot express (params: null, arrays).
func postRaw(t *testing.T, url, body string) (int, *env, []byte) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", body, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var e env
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("parse response %s: %v", raw, err)
	}
	return resp.StatusCode, &e, raw
}

// TestPartDecodeTypeConfusionRefused pins that Part's decoder enforces
// the exactly-one-content rule and refuses mistyped discriminators at
// the decode boundary: no coercion, no empty part through.
func TestPartDecodeTypeConfusionRefused(t *testing.T) {
	bad := map[string]string{
		"no discriminator":     `{}`,
		"text and url":         `{"text":"a","url":"b"}`,
		"text and data":        `{"text":"a","data":1}`,
		"three discriminators": `{"text":"a","raw":"aGk=","url":"u"}`,
		"text is a number":     `{"text":123}`,
		"url is an array":      `{"url":["x"]}`,
		"raw not base64":       `{"raw":"!!!not-base64!!!"}`,
		"raw is a number":      `{"raw":5}`,
		"only null data":       `{"data":null}`,
		"only null text":       `{"text":null}`,
	}
	for name, body := range bad {
		var p Part
		if err := json.Unmarshal([]byte(body), &p); err == nil {
			t.Errorf("%s: %s decoded without error (kind %q)", name, body, p.Kind())
		}
	}
	// Contract pin (types.go): a null sibling does not count as content,
	// so a valid discriminator plus null siblings is accepted.
	for _, body := range []string{`{"text":"x","raw":null}`, `{"data":{"skill":"x"},"url":null}`} {
		var p Part
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Errorf("%s: refused (%v), want accepted with null siblings ignored", body, err)
		}
	}
}

// TestBadParamsShapesRefused pins that malformed params — non-object
// shapes, mistyped part fields, and unparsable client-borne timestamps
// — are refused with -32602 at the decode boundary, and a refused send
// leaves no half-built task behind.
func TestBadParamsShapesRefused(t *testing.T) {
	h := newHarness(t, nil)
	for name, body := range map[string]string{
		"params null":   `{"jsonrpc":"2.0","id":1,"method":"GetTask","params":null}`,
		"params array":  `{"jsonrpc":"2.0","id":1,"method":"GetTask","params":[1,2]}`,
		"params string": `{"jsonrpc":"2.0","id":1,"method":"GetTask","params":"x"}`,
	} {
		status, e, _ := postRaw(h.t, h.ts.URL, body)
		if status != http.StatusOK || e.Error == nil || e.Error.Code != CodeInvalidParams {
			t.Errorf("%s: status=%d err=%+v, want 200 + -32602", name, status, e.Error)
		}
	}

	// Mistyped part discriminator through the full SendMessage path.
	_, e, _ := h.call("alice", MethodSendMessage, map[string]any{
		"message": map[string]any{"role": "ROLE_USER", "parts": []any{map[string]any{"text": 123}}},
	})
	if e.Error == nil || e.Error.Code != CodeInvalidParams {
		t.Errorf("mistyped part: err=%+v, want -32602", e.Error)
	}
	_, total, _ := h.srv.store.ListTasks(context.Background(), "alice", ListQuery{})
	if total != 0 {
		t.Fatalf("refused send left %d tasks behind", total)
	}

	// Client-borne timestamps must parse as RFC 3339 strings.
	for _, bad := range []any{"2026-13-01T00:00:00Z", "not-a-time", 1756682400, true} {
		_, e, _ := h.call("alice", MethodListTasks, map[string]any{"statusTimestampAfter": bad})
		if e.Error == nil || e.Error.Code != CodeInvalidParams {
			t.Errorf("statusTimestampAfter %v: err=%+v, want -32602", bad, e.Error)
		}
	}
	_, e, _ = h.call("alice", MethodListTasks, map[string]any{"statusTimestampAfter": "2026-09-01T00:00:00Z"})
	if e.Error != nil {
		t.Errorf("valid statusTimestampAfter refused: %+v", e.Error)
	}
}

// echoSendParams is valid SendMessage params routed to the echo skill —
// what a smuggled last-wins method needs to actually dispatch.
const echoSendParams = `{"message":{"role":"ROLE_USER","parts":[{"text":"hi"}],"metadata":{"skill":"echo"}}}`

// envelopeRejected drives one raw JSON-RPC envelope body and fails
// unless the server refuses it before dispatch: ServeHTTP decodes the
// envelope through handler.UnmarshalStrict, which refuses duplicate
// and case-folded top-level keys (stdlib json keeps the LAST duplicate
// and matches struct tags case-insensitively, so an intermediary
// validating the first occurrence sees a benign read while the
// executor dispatches the smuggled method).
func envelopeRejected(t *testing.T, name, body string) {
	t.Helper()
	h := newHarness(t, nil)
	var ran atomic.Bool
	h.setHandler(func(_ context.Context, task TaskContext) error {
		ran.Store(true)
		return task.Complete(TextPart("smuggled"))
	})

	req := httptest.NewRequest(http.MethodPost, h.ts.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Owner", "alice")
	rec := httptest.NewRecorder()
	h.srv.ServeHTTP(rec, req)

	var e env
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	rejected := rec.Code == http.StatusBadRequest || e.Error != nil
	if !rejected {
		t.Fatalf("SECURITY: [strict-json] %s: duplicated top-level key body was accepted and dispatched (status=%d body=%s) — stdlib json's silent last-key-wins lets the second method run while a first-occurrence validator saw %q; want the parse rejected like every Bind consumer", name, rec.Code, rec.Body.String(), MethodGetTask)
	}
	if ran.Load() {
		t.Fatalf("SECURITY: [strict-json] %s: the skill handler ran behind a duplicated/case-folded method key — the smuggled method was dispatched", name)
	}
}

// Property: the JSON-RPC envelope refuses exact-duplicate top-level
// keys, so no first-occurrence parser (proxy, WAF, audit logger) can
// disagree with the executor's last-key-wins decode.
func TestRejectsDuplicateEnvelopeKeys(t *testing.T) {
	envelopeRejected(t, "duplicate method key",
		`{"jsonrpc":"2.0","id":"dup-1","method":"`+MethodGetTask+`","method":"`+MethodSendMessage+`","params":`+echoSendParams+`}`)
}

// "Method"/"method" case-fold onto the same struct field via stdlib
// json's tag-insensitive match — a duplicate modulo folding. Survives a
// dedup-only fix.
func TestRejectsCaseFoldedEnvelopeKeys(t *testing.T) {
	envelopeRejected(t, "case-folded method key",
		`{"jsonrpc":"2.0","id":"fold-1","Method":"`+MethodGetTask+`","method":"`+MethodSendMessage+`","params":`+echoSendParams+`}`)
}

// TestPageTokenTamperRefused pins that a pageToken is refused unless it
// is base64 of a non-negative decimal, at both paged surfaces:
// ListTasks and ListTaskPushNotificationConfigs.
func TestPageTokenTamperRefused(t *testing.T) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	shapes := []string{
		"!!!not-base64!!!",          // not base64 at all
		enc("abc"),                  // base64, not a number
		enc("-5"),                   // negative offset
		enc(" 5"),                   // whitespace-padded number
		enc("99999999999999999999"), // overflows int
	}
	h := newHarness(t, func(c *Config) { c.Push.AllowPrivate = true })
	t1 := h.send("alice")
	h.send("alice")
	for i, id := range []string{"p1", "p2"} {
		_, e, _ := h.call("alice", MethodCreateTaskPushNotificationConfig, map[string]any{
			"taskId": t1.ID, "url": "https://127.0.0.1:9/hook", "id": id,
		})
		if e.Error != nil {
			t.Fatalf("seed push config %d: %+v", i, e.Error)
		}
	}
	surfaces := []struct {
		name   string
		method string
		params func(tok string) map[string]any
	}{
		{"ListTasks", MethodListTasks, func(tok string) map[string]any { return map[string]any{"pageToken": tok} }},
		{"ListTaskPushNotificationConfigs", MethodListTaskPushNotificationConfigs, func(tok string) map[string]any {
			return map[string]any{"taskId": t1.ID, "pageToken": tok}
		}},
	}
	for _, s := range surfaces {
		for _, tok := range shapes {
			_, e, _ := h.call("alice", s.method, s.params(tok))
			if e.Error == nil || e.Error.Code != CodeInvalidParams || e.Error.Message != "invalid pageToken" {
				t.Errorf("%s token %q: err=%+v, want -32602 invalid pageToken", s.name, tok, e.Error)
			}
		}
		// A well-formed token still pages normally on both surfaces.
		_, e, _ := h.call("alice", s.method, s.params(enc("0")))
		if e.Error != nil {
			t.Errorf("%s with valid token: %+v", s.name, e.Error)
		}
	}
}

// TestForgedPageTokenStaysScoped pins that a well-formed but forged
// cursor (the encoding is opaque, not signed) cannot read past the
// caller's own rows: an offset beyond the total yields an empty page
// with no successor token, and another owner's token yields that
// owner's page, never alice's rows.
func TestForgedPageTokenStaysScoped(t *testing.T) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	h := newHarness(t, func(c *Config) { c.Push.AllowPrivate = true })
	task := h.send("alice")
	h.send("alice")

	// ListTasks: offset far beyond alice's total.
	_, e, _ := h.call("alice", MethodListTasks, map[string]any{"pageToken": enc("50")})
	if e.Error != nil {
		t.Fatalf("forged offset refused: %+v", e.Error)
	}
	var list ListTasksResponse
	if err := json.Unmarshal(e.Result, &list); err != nil {
		t.Fatalf("parse %s: %v", e.Result, err)
	}
	if len(list.Tasks) != 0 || list.NextPageToken != "" {
		t.Errorf("forged offset leaked rows or a successor: %+v", list)
	}
	// Bob replaying alice's real cursor sees only bob's (empty) page.
	_, e, _ = h.call("bob", MethodListTasks, map[string]any{"pageToken": enc("1")})
	if e.Error != nil {
		t.Fatalf("cross-owner token refused: %+v", e.Error)
	}
	var bobList ListTasksResponse
	if err := json.Unmarshal(e.Result, &bobList); err != nil {
		t.Fatalf("parse %s: %v", e.Result, err)
	}
	if len(bobList.Tasks) != 0 || bobList.TotalSize != 0 {
		t.Errorf("bob's page with alice's cursor = %+v, want empty and his own total", bobList)
	}

	// Same property on the push-config surface.
	if err := h.srv.store.CreatePushConfig(context.Background(), &PushConfigRecord{
		Config: PushNotificationConfig{ID: "p1", TaskID: task.ID, URL: "https://127.0.0.1:9/hook"},
		Owner:  "alice", CreatedAt: t0,
	}); err != nil {
		t.Fatalf("seed push: %v", err)
	}
	_, e, _ = h.call("alice", MethodListTaskPushNotificationConfigs, map[string]any{"taskId": task.ID, "pageToken": enc("50")})
	if e.Error != nil {
		t.Fatalf("push forged offset refused: %+v", e.Error)
	}
	var pushList ListTaskPushNotificationConfigsResponse
	if err := json.Unmarshal(e.Result, &pushList); err != nil {
		t.Fatalf("parse %s: %v", e.Result, err)
	}
	if len(pushList.Configs) != 0 || pushList.NextPageToken != "" {
		t.Errorf("push forged offset = %+v, want empty page", pushList)
	}
}

// TestHistoryLengthEveryTaskSurface pins that historyLength trims only
// the response copy (store keeps the full history) at every surface
// that returns a task: GetTask, ListTasks, SendMessage, and the
// SendStreamingMessage snapshot — and that 0 or negative means no
// history, never an error.
func TestHistoryLengthEveryTaskSurface(t *testing.T) {
	h := newHarness(t, nil)
	pause := func(_ context.Context, tc TaskContext) error {
		return tc.RequireInput(TextPart("need input"))
	}

	// GetTask surface: paused task holds 2 history messages.
	h.setHandler(pause)
	task := h.send("alice")
	_, e, _ := h.call("alice", MethodGetTask, map[string]any{"id": task.ID, "historyLength": 1})
	if e.Error != nil {
		t.Fatalf("GetTask: %+v", e.Error)
	}
	var got Task
	if err := json.Unmarshal(e.Result, &got); err != nil {
		t.Fatalf("parse %s: %v", e.Result, err)
	}
	if len(got.History) != 1 || statusText(&got) != "need input" {
		t.Fatalf("GetTask trimmed history = %d msgs, want the newest 1", len(got.History))
	}

	// ListTasks surface, same task.
	_, e, _ = h.call("alice", MethodListTasks, map[string]any{"historyLength": 1})
	if e.Error != nil {
		t.Fatalf("ListTasks: %+v", e.Error)
	}
	var list ListTasksResponse
	if err := json.Unmarshal(e.Result, &list); err != nil {
		t.Fatalf("parse %s: %v", e.Result, err)
	}
	if len(list.Tasks) == 0 || len(list.Tasks[0].History) != 1 {
		t.Fatalf("ListTasks trimmed history = %+v, want newest 1 per task", list.Tasks)
	}

	// SendMessage surface: the final task's history trimmed in the
	// result while the run already appended the agent answer.
	h.setHandler(echoHandler)
	sent := h.send("alice", map[string]any{"historyLength": 1})
	if len(sent.History) != 1 || statusText(sent) != "done" {
		t.Fatalf("SendMessage trimmed history = %d msgs, want newest 1", len(sent.History))
	}

	// SendStreamingMessage surface: the snapshot of a resumed (paused)
	// task is trimmed before it is framed.
	h.setHandler(pause)
	paused := h.send("alice")
	h.setHandler(echoHandler)
	r := h.openStream("alice", MethodSendStreamingMessage, map[string]any{
		"message":       map[string]any{"taskId": paused.ID, "role": "ROLE_USER", "parts": []any{map[string]any{"text": "resume"}}},
		"configuration": map[string]any{"historyLength": 1},
	})
	_, first := r.nextResult(3 * time.Second)
	if first.Task == nil || len(first.Task.History) != 1 {
		t.Fatalf("streaming snapshot history = %+v, want trimmed to 1", first.Task)
	}
	r.eof(3 * time.Second)

	// Zero and negative mean no history, never an error.
	for _, n := range []int{0, -3} {
		_, e, _ := h.call("alice", MethodGetTask, map[string]any{"id": task.ID, "historyLength": n})
		if e.Error != nil {
			t.Fatalf("historyLength %d: %+v", n, e.Error)
		}
		var got Task
		if err := json.Unmarshal(e.Result, &got); err != nil {
			t.Fatalf("parse %s: %v", e.Result, err)
		}
		if len(got.History) != 0 {
			t.Errorf("historyLength %d returned %d msgs, want 0", n, len(got.History))
		}
	}

	// The store still holds the full history after every trimmed reply.
	_, e, _ = h.call("alice", MethodGetTask, map[string]any{"id": task.ID})
	if e.Error != nil {
		t.Fatalf("GetTask full: %+v", e.Error)
	}
	if err := json.Unmarshal(e.Result, &got); err != nil {
		t.Fatalf("parse %s: %v", e.Result, err)
	}
	if len(got.History) != 2 {
		t.Fatalf("stored history = %d msgs after trimmed replies, want the full 2", len(got.History))
	}
}

// TestNewUUIDShapeAndUniqueness pins the id generator's shape contract:
// lowercase RFC 4122 v4 (version nibble 4, variant 10x) with no
// collision across a large draw.
func TestNewUUIDShapeAndUniqueness(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := make(map[string]struct{}, 4096)
	for range 4096 {
		id := newUUID()
		if !re.MatchString(id) {
			t.Fatalf("newUUID = %q, not an RFC 4122 v4 form", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("newUUID collided on %q", id)
		}
		seen[id] = struct{}{}
	}
}

// TestMintSitesUseUniqueV4IDs pins that every id the server mints for a
// client — task, context, inbound message, artifact, push config — is a
// fresh v4 UUID, so no mint site reuses another's id.
func TestMintSitesUseUniqueV4IDs(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	h := newHarness(t, func(c *Config) { c.Push.AllowPrivate = true })
	h.srv.newID = newUUID // real generator; the fixed clock stays

	task := h.send("alice") // message carries no messageId: server mints
	ids := map[string]bool{}
	for name, id := range map[string]string{
		"task id":    task.ID,
		"context id": task.ContextID,
		"message id": task.History[0].MessageID,
	} {
		if !re.MatchString(id) {
			t.Errorf("%s minted %q, not a v4 UUID", name, id)
		}
		if ids[id] {
			t.Errorf("%s minted %q, already minted at another site", name, id)
		}
		ids[id] = true
	}

	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Artifact(Artifact{Parts: []Part{TextPart("x")}}, false); err != nil {
			return err
		}
		return tc.Complete(TextPart("done"))
	})
	withArt := h.send("alice")
	fresh, err := h.srv.store.GetTask(context.Background(), "alice", withArt.ID)
	if err != nil || len(fresh.Task.Artifacts) != 1 {
		t.Fatalf("artifact task: %+v (%v)", fresh, err)
	}
	artID := fresh.Task.Artifacts[0].ArtifactID
	if !re.MatchString(artID) || ids[artID] {
		t.Errorf("artifact id %q not a fresh v4 UUID", artID)
	}
	ids[artID] = true

	_, e, _ := h.call("alice", MethodCreateTaskPushNotificationConfig, map[string]any{
		"taskId": withArt.ID, "url": "https://127.0.0.1:9/hook", // no id: server mints
	})
	if e.Error != nil {
		t.Fatalf("create push config: %+v", e.Error)
	}
	var cfg PushNotificationConfig
	if err := json.Unmarshal(e.Result, &cfg); err != nil {
		t.Fatalf("parse %s: %v", e.Result, err)
	}
	if !re.MatchString(cfg.ID) || ids[cfg.ID] {
		t.Errorf("push config id %q not a fresh v4 UUID", cfg.ID)
	}
}
