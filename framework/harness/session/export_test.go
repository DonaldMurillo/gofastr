package session

import (
	"archive/zip"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// fakeStore is a minimal Store for the export tests. Real
// store.sqlite is tested elsewhere.
type fakeStore struct {
	events []control.EventEnvelope
}

func (f *fakeStore) AppendEvent(_ context.Context, env control.EventEnvelope) error {
	f.events = append(f.events, env)
	return nil
}

func (f *fakeStore) EventsSince(_ context.Context, _ ids.SessionID, since uint64, limit int) ([]control.EventEnvelope, error) {
	var out []control.EventEnvelope
	for _, e := range f.events {
		if e.ID > since {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeStore) ListPastSessions(_ context.Context, _ int) ([]PastSession, error) {
	return nil, nil
}
func (f *fakeStore) RecordToolIntent(_ context.Context, _ ToolIntent) error   { return nil }
func (f *fakeStore) RecordToolOutcome(_ context.Context, _ ToolOutcome) error { return nil }
func (f *fakeStore) OrphanIntents(_ context.Context, _ ids.SessionID) ([]ToolIntent, error) {
	return nil, nil
}
func (f *fakeStore) ApplyRetention(_ context.Context, _ time.Duration) (int64, error) { return 0, nil }
func (f *fakeStore) Close() error                                                     { return nil }

func TestExportBundleProducesZip(t *testing.T) {
	store := &fakeStore{}
	sess := ids.NewSessionID()
	env, _ := control.EncodeEvent(1, control.TextDelta{Text: "hello"}, sess, ids.NewClientID(), time.Now())
	_ = store.AppendEvent(context.Background(), env)

	out := filepath.Join(t.TempDir(), "bundle.zip")
	e := &ExportBundle{
		Store:   store,
		Session: sess,
		Profile: "default",
		Model:   "zai:glm-5.1",
		Level:   RedactStandard,
		OutPath: out,
	}
	path, err := e.Write(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	names := map[string]bool{}
	for _, f := range r.File {
		names[f.Name] = true
	}
	if !names["bundle.json"] || !names["events.jsonl"] {
		t.Errorf("zip contents = %v", names)
	}
}

func TestExportStrictReplacesPayload(t *testing.T) {
	store := &fakeStore{}
	sess := ids.NewSessionID()
	env, _ := control.EncodeEvent(1, control.TextDelta{Text: "secret"}, sess, ids.NewClientID(), time.Now())
	_ = store.AppendEvent(context.Background(), env)

	out := filepath.Join(t.TempDir(), "bundle.zip")
	e := &ExportBundle{
		Store:   store,
		Session: sess,
		Level:   RedactStrict,
		OutPath: out,
	}
	_, err := e.Write(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	body := readEventsFromBundle(t, out)
	if strings.Contains(body, "secret") {
		t.Errorf("strict mode leaked content: %q", body)
	}
	if !strings.Contains(body, `"redacted":"strict"`) {
		t.Errorf("strict marker missing: %q", body)
	}
}

func TestExportMaintainerIncludesReport(t *testing.T) {
	store := &fakeStore{}
	sess := ids.NewSessionID()
	env, _ := control.EncodeEvent(1, control.TextDelta{
		Text: "AKIAEXAMPLEKEY123456 and ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, sess, ids.NewClientID(), time.Now())
	_ = store.AppendEvent(context.Background(), env)

	out := filepath.Join(t.TempDir(), "bundle.zip")
	e := &ExportBundle{
		Store:   store,
		Session: sess,
		Level:   RedactMaintainer,
		OutPath: out,
	}
	_, err := e.Write(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	r, _ := zip.OpenReader(out)
	defer r.Close()
	hasReport := false
	for _, f := range r.File {
		if f.Name == "redactions.json" {
			hasReport = true
		}
	}
	if !hasReport {
		t.Error("redactions.json missing at maintainer level")
	}
	body := readEventsFromBundle(t, out)
	if strings.Contains(body, "AKIAEXAMPLEKEY123456") {
		t.Error("AWS key leaked at maintainer level")
	}
	if !strings.Contains(body, "«redacted:aws-access-key»") {
		t.Errorf("AWS key not replaced: %q", body)
	}
}

func TestDeepRedactCountsHits(t *testing.T) {
	out, hits := deepRedact("AKIAABCDEFGHIJKLMNOP me@example.com")
	if !strings.Contains(out, "«redacted:") {
		t.Error("no replacement performed")
	}
	if hits["aws-access-key"] != 1 {
		t.Errorf("aws hit count = %d", hits["aws-access-key"])
	}
	if hits["email"] != 1 {
		t.Errorf("email hit count = %d", hits["email"])
	}
}

func readEventsFromBundle(t *testing.T, path string) string {
	t.Helper()
	r, _ := zip.OpenReader(path)
	defer r.Close()
	for _, f := range r.File {
		if f.Name == "events.jsonl" {
			rc, _ := f.Open()
			data, _ := io.ReadAll(rc)
			_ = rc.Close()
			return string(data)
		}
	}
	t.Fatal("events.jsonl not found")
	return ""
}
