package sqlite

import (
	"archive/zip"
	"bufio"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/session"
)

// An export holds every event of the session. The store caps one
// EventsSince call at 10,000 rows, and the export made one call, so a
// longer session exported a bundle that looked complete minus its tail.
func TestExportHoldsEveryEvent(t *testing.T) {
	s := newStore(t)
	sess := ids.NewSessionID()
	who := ids.NewClientID()
	const n = 10_050
	for i := uint64(1); i <= n; i++ {
		env, err := control.EncodeEvent(i, control.TextDelta{Text: "x"}, sess, who, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := s.AppendEvent(context.Background(), env); err != nil {
			t.Fatal(err)
		}
	}
	e := &session.ExportBundle{
		Store:   s,
		Session: sess,
		Level:   session.RedactStandard,
		OutPath: filepath.Join(t.TempDir(), "bundle.zip"),
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
	for _, f := range r.File {
		if f.Name != "events.jsonl" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		lines := 0
		sc := bufio.NewScanner(rc)
		for sc.Scan() {
			lines++
		}
		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}
		if lines != n {
			t.Fatalf("events.jsonl has %d events, want %d", lines, n)
		}
		return
	}
	t.Fatal("bundle has no events.jsonl")
}
