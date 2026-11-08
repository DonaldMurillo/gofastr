package log

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A backup index must parse exactly: "app.log.01" and "app.log.1" are
// two distinct files (logrotate and friends write zero-padded indexes),
// not two spellings of backup #1. The shift used to treat them as the
// same slot: both parsed to n=1, one was renamed onto the other's
// target and a whole backup was silently overwritten (#657 parked
// bugs).
func TestRotationTreatsZeroPaddedBackupsAsForeign(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	foreign := filepath.Join(dir, "app.log.01")
	one := filepath.Join(dir, "app.log.1")
	if err := os.WriteFile(foreign, []byte("foreign-tool-backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(one, []byte("backup-one"), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := FileSink(path, FileOpts{MaxSize: 20, MaxBackups: 5})
	if err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if err := s.Write([]byte(`xxxxxxxxxx`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// The zero-padded file belongs to nobody in our series: still there,
	// untouched.
	got, err := os.ReadFile(foreign)
	if err != nil {
		t.Fatalf("app.log.01 was moved or removed by our rotation: %v", err)
	}
	if string(got) != "foreign-tool-backup" {
		t.Fatalf("app.log.01 content = %q, want the foreign tool's bytes untouched", got)
	}

	// Our own series shifted: the active log's chunks sit in the low
	// slots and the old backup one in exactly one higher slot, its
	// content intact (not overwritten by the foreign series).
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	withBackup := ""
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "app.log.") {
			continue
		}
		suffix := strings.TrimPrefix(name, "app.log.")
		if n, err := strconv.Atoi(suffix); err != nil || strconv.Itoa(n) != suffix {
			continue // the foreign .01, asserted above
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "backup-one") {
			if withBackup != "" {
				t.Fatalf("backup-one appears in both %s and %s", withBackup, name)
			}
			withBackup = name
		}
	}
	if withBackup == "" {
		t.Fatal("backup-one vanished: the rotation overwrote the old backup")
	}
}
