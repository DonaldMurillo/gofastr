package session

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// readBundleFile returns one file's bytes from an export zip.
func readBundleFile(t *testing.T, path, name string) []byte {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		body, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	t.Fatalf("bundle has no %s", name)
	return nil
}

// The maintainer pass redacts decoded strings: matching the serialized
// JSON caught the "u003c" of a < escape as part of an email and
// left a broken escape behind, and the event vanished from the bundle.
func TestDeepRedactKeepsEventsValid(t *testing.T) {
	store := &fakeStore{}
	sess := ids.NewSessionID()
	env, _ := control.EncodeEvent(1, control.TextDelta{Text: "mail <dom@x.com> now"}, sess, ids.NewClientID(), time.Now())
	_ = store.AppendEvent(context.Background(), env)
	e := &ExportBundle{Store: store, Session: sess, Level: RedactMaintainer, OutPath: filepath.Join(t.TempDir(), "b.zip")}
	path, err := e.Write(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	body := readBundleFile(t, path, "events.jsonl")
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 1 || lines[0] == "" {
		t.Fatalf("events.jsonl = %q, want one event", body)
	}
	var got control.EventEnvelope
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatal(err)
	}
	var td control.TextDelta
	if err := json.Unmarshal(got.Payload, &td); err != nil {
		t.Fatal(err)
	}
	if td.Text != "mail <«redacted:email»> now" {
		t.Errorf("text = %q", td.Text)
	}
}

// An event that cannot be written into the bundle is counted in the
// manifest, never dropped without a trace.
func TestExportCountsSkippedEvents(t *testing.T) {
	store := &fakeStore{}
	sess := ids.NewSessionID()
	good, _ := control.EncodeEvent(1, control.TextDelta{Text: "ok"}, sess, ids.NewClientID(), time.Now())
	bad := good
	bad.ID = 2
	bad.Payload = json.RawMessage(`{"text":"cut`)
	_ = store.AppendEvent(context.Background(), good)
	_ = store.AppendEvent(context.Background(), bad)
	e := &ExportBundle{Store: store, Session: sess, Level: RedactStandard, OutPath: filepath.Join(t.TempDir(), "b.zip")}
	path, err := e.Write(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Skipped int `json:"skipped_events"`
	}
	if err := json.Unmarshal(readBundleFile(t, path, "bundle.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Skipped != 1 {
		t.Errorf("skipped_events = %d, want 1", manifest.Skipped)
	}
}
