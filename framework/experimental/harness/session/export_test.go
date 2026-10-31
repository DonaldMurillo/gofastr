package session

import (
	"archive/zip"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// fakeStore is a minimal Store for the export tests. Real
// store.sqlite is tested elsewhere. maxPage, when set, caps every
// EventsSince page below what the caller asked for, the way a store
// with its own maximum does.
type fakeStore struct {
	events  []control.EventEnvelope
	maxPage int
}

func (f *fakeStore) AppendEvent(_ context.Context, env control.EventEnvelope) error {
	f.events = append(f.events, env)
	return nil
}

func (f *fakeStore) EventsSince(_ context.Context, _ ids.SessionID, since uint64, limit int) ([]control.EventEnvelope, error) {
	var out []control.EventEnvelope
	for _, e := range f.events {
		if (limit > 0 && len(out) == limit) || (f.maxPage > 0 && len(out) == f.maxPage) {
			break
		}
		if e.ID > since {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeStore) ListPastSessions(_ context.Context, _ int, _ []ids.SessionID) ([]PastSession, error) {
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

// The export reads the session page by page until a page comes back
// empty, so a store whose pages are smaller than the export asked for
// still exports every event.
func TestExportPagesPastAStoreCap(t *testing.T) {
	store := &fakeStore{maxPage: 3}
	sess := ids.NewSessionID()
	for i := uint64(1); i <= 7; i++ {
		env, _ := control.EncodeEvent(i, control.TextDelta{Text: "x"}, sess, ids.NewClientID(), time.Now())
		_ = store.AppendEvent(context.Background(), env)
	}
	e := &ExportBundle{Store: store, Session: sess, Level: RedactStandard, OutPath: filepath.Join(t.TempDir(), "b.zip")}
	path, err := e.Write(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name != "events.jsonl" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(string(body), "\n"); got != 7 {
			t.Fatalf("events.jsonl has %d events, want 7", got)
		}
		return
	}
	t.Fatal("bundle has no events.jsonl")
}

// stuckStore returns its whole log whatever since says.
type stuckStore struct{ fakeStore }

func (s *stuckStore) EventsSince(_ context.Context, _ ids.SessionID, _ uint64, _ int) ([]control.EventEnvelope, error) {
	return s.events, nil
}

// A store that does not advance past since fails the export instead of
// looping on the same page forever.
func TestExportRefusesAStuckStore(t *testing.T) {
	store := &stuckStore{}
	sess := ids.NewSessionID()
	env, _ := control.EncodeEvent(1, control.TextDelta{Text: "x"}, sess, ids.NewClientID(), time.Now())
	_ = store.AppendEvent(context.Background(), env)
	e := &ExportBundle{Store: store, Session: sess, Level: RedactStandard, OutPath: filepath.Join(t.TempDir(), "b.zip")}
	done := make(chan error, 1)
	go func() {
		_, err := e.Write(context.Background())
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("export of a store that ignores since succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("export looped on a store that ignores since")
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
		Text: "AKIAEXAMPLEKEY123456 and ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", // nosecret: fake fixture proving export redaction
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
	if strings.Contains(body, "AKIAEXAMPLEKEY123456") { // nosecret: fake fixture
		t.Error("AWS key leaked at maintainer level")
	}
	if !strings.Contains(body, "«redacted:aws-access-key»") {
		t.Errorf("AWS key not replaced: %q", body)
	}
}

func TestDeepRedactCountsHits(t *testing.T) {
	out, hits := deepRedact("AKIAABCDEFGHIJKLMNOP me@example.com") // nosecret: fake fixture
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
