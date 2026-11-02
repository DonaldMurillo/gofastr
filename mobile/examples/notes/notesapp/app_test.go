package notesapp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-mobile/mobilehost"
)

func startNotes(t *testing.T, dir string) *mobilehost.Host {
	t.Helper()
	_, h, err := Build(dir, "")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := h.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return h
}

func stopNotes(t *testing.T, h *mobilehost.Host) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func do(t *testing.T, h *mobilehost.Host, method, path, ctype, body string) *mobilehost.Response {
	t.Helper()
	hdr := http.Header{"Accept": {"text/html,application/json"}, "Sec-Fetch-Site": {"same-origin"}, "Origin": {mobilehost.DefaultOrigin}}
	if ctype != "" {
		hdr.Set("Content-Type", ctype)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := h.Do(ctx, &mobilehost.Request{Method: method, URL: mobilehost.DefaultOrigin + path, Header: hdr, Body: []byte(body)})
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return r
}

// The whole app answers in-process: the list page renders, a note
// created through the entity's REST route shows up in it, and the row is
// stamped with the host's local identity, which survives a restart on the
// same data dir (the "app was killed and relaunched" case on a phone).
func TestNotesInProcess(t *testing.T) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	dir := t.TempDir()
	h := startNotes(t, dir)

	page := do(t, h, "GET", "/", "", "")
	if page.Status != 200 || !strings.Contains(string(page.Body), "<html") {
		t.Fatalf("GET / = %d, body starts %.200q", page.Status, page.Body)
	}

	created := do(t, h, "POST", "/api/notes", "application/json", `{"title":"From the phone","body":"in-process"}`)
	if created.Status != 201 && created.Status != 200 {
		t.Fatalf("POST /api/notes = %d %s", created.Status, created.Body)
	}
	var row map[string]any
	if err := json.Unmarshal(created.Body, &row); err != nil {
		t.Fatalf("decode create: %v (%s)", err, created.Body)
	}
	if d, ok := row["data"].(map[string]any); ok {
		row = d
	}
	firstOwner, _ := row["userId"].(string)
	if len(firstOwner) != 32 {
		t.Fatalf("created row owner = %q, want the 32-hex local identity; row %v", firstOwner, row)
	}

	list := do(t, h, "GET", "/notes", "", "")
	if !strings.Contains(string(list.Body), "From the phone") {
		t.Fatalf("list page does not show the new note")
	}
	stopNotes(t, h)

	owner.SetExtractor(nil)
	h2 := startNotes(t, dir)
	defer stopNotes(t, h2)
	again := do(t, h2, "GET", "/api/notes", "", "")
	if !strings.Contains(string(again.Body), firstOwner) || !strings.Contains(string(again.Body), "From the phone") {
		t.Fatalf("after relaunch the note (owner %s) is gone: %s", firstOwner, again.Body)
	}
}

// Requests for another origin never reach the app.
func TestRefusesForeignOrigin(t *testing.T) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	h := startNotes(t, t.TempDir())
	defer stopNotes(t, h)
	_, err := h.Do(context.Background(), &mobilehost.Request{Method: "GET", URL: "https://example.com/"})
	if err == nil {
		t.Fatal("a foreign-origin request was served")
	}
}
