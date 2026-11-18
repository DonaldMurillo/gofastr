package localrun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterRefusals(t *testing.T) {
	t.Setenv(EnvDir, t.TempDir())
	if _, err := Register(Record{}); err == nil {
		t.Error("Register accepted a record with no URL")
	}
	if _, err := Register(Record{URL: "http://127.0.0.1:1", PID: 12345678901}); err == nil {
		t.Error("Register accepted a pid outside the ID pattern")
	}
}

func TestListSkipsOversizedAndLooseRecords(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	id := "1-0000000a"
	pad := strings.Repeat(" ", maxRecordBytes)
	body := `{"version":1,"id":"` + id + `","name":"x","pid":1,"url":"http://127.0.0.1:1","started":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, id+recordExt), []byte(body+pad), 0o600); err != nil {
		t.Fatal(err)
	}
	id2 := "1-0000000b"
	extra := `{"version":1,"id":"` + id2 + `","name":"x","pid":1,"url":"http://127.0.0.1:1","started":"2026-01-01T00:00:00Z","surprise":1}`
	if err := os.WriteFile(filepath.Join(dir, id2+recordExt), []byte(extra), 0o600); err != nil {
		t.Fatal(err)
	}
	recs, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("List = %v, want an oversized record and one with an unknown field skipped", recs)
	}
}

func TestOrphanSidecarKeptBesideItsRecord(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	unregister, err := Register(Record{URL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	defer unregister()
	recs, err := List()
	if err != nil || len(recs) != 1 {
		t.Fatalf("List = %v, %v", recs, err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dropOrphanStart(root, []string{recs[0].ID + startExt}, recs[0].ID)
	if _, err := os.Stat(filepath.Join(dir, recs[0].ID+startExt)); err != nil {
		t.Fatalf("a live run's sidecar was removed: %v", err)
	}
	dead := "999999999-0000000c"
	if err := os.WriteFile(filepath.Join(dir, dead+startExt), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	dropOrphanStart(root, []string{dead + recordExt, dead + startExt}, dead)
	if _, err := os.Stat(filepath.Join(dir, dead+startExt)); err != nil {
		t.Fatalf("a sidecar whose record is still listed was removed: %v", err)
	}
}

func TestForcedSpellings(t *testing.T) {
	for _, v := range []string{"on", "1", "TRUE", " yes "} {
		t.Setenv(EnvRuns, v)
		if !Forced() {
			t.Errorf("Forced() false for %q", v)
		}
	}
}
