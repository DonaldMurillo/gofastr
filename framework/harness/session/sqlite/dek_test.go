package sqlite

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

func makeKey() []byte {
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	return k
}

func TestOpenWithKEKRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sessions.db")
	kek := makeKey()

	s, err := OpenWithKEK(dbPath, kek)
	if err != nil {
		t.Fatal(err)
	}
	sess := ids.NewSessionID()
	env, _ := control.EncodeEvent(1, control.TextDelta{Text: "kek-protected"}, sess, ids.NewClientID(), time.Now())
	_ = s.AppendEvent(context.Background(), env)
	if err := s.CloseEncrypted(); err != nil {
		t.Fatal(err)
	}

	// Reopen with the same KEK.
	s2, err := OpenWithKEK(dbPath, kek)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.CloseEncrypted()
	got, _ := s2.EventsSince(context.Background(), sess, 0, 0)
	if len(got) != 1 || !strings.Contains(string(got[0].Payload), "kek-protected") {
		t.Errorf("payload missing: %+v", got)
	}
}

func TestRotateKEKDoesNotRewriteDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sessions.db")
	oldKEK := makeKey()

	s, _ := OpenWithKEK(dbPath, oldKEK)
	sess := ids.NewSessionID()
	env, _ := control.EncodeEvent(1, control.TextDelta{Text: "rotate"}, sess, ids.NewClientID(), time.Now())
	_ = s.AppendEvent(context.Background(), env)
	_ = s.CloseEncrypted()

	// Capture .enc file mtime to verify it isn't touched by rotation.
	encStat, _ := os.Stat(dbPath + ".enc")
	mtime := encStat.ModTime()

	// Rotate.
	time.Sleep(50 * time.Millisecond)
	newKEK := makeKey()
	if err := RotateKEK(dbPath, oldKEK, newKEK); err != nil {
		t.Fatal(err)
	}

	// .enc file unchanged.
	encStat2, _ := os.Stat(dbPath + ".enc")
	if !encStat2.ModTime().Equal(mtime) {
		t.Errorf(".enc was touched during rotation: %v vs %v", encStat2.ModTime(), mtime)
	}
	// .dek file updated.
	dekStat, _ := os.Stat(dbPath + ".dek")
	if dekStat.ModTime().Equal(mtime) {
		t.Error(".dek was NOT updated during rotation")
	}

	// Old KEK no longer works.
	if _, err := OpenWithKEK(dbPath, oldKEK); err == nil {
		t.Error("old KEK should no longer decrypt DEK")
	}
	// New KEK does.
	s2, err := OpenWithKEK(dbPath, newKEK)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.CloseEncrypted()
	got, _ := s2.EventsSince(context.Background(), sess, 0, 0)
	if len(got) != 1 {
		t.Errorf("got %d events after rotation", len(got))
	}
}

func TestExportImportDEK(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sessions.db")
	kek := makeKey()

	s, _ := OpenWithKEK(dbPath, kek)
	sess := ids.NewSessionID()
	env, _ := control.EncodeEvent(1, control.TextDelta{Text: "exported"}, sess, ids.NewClientID(), time.Now())
	_ = s.AppendEvent(context.Background(), env)
	_ = s.CloseEncrypted()

	recipient := makeKey()
	exportPath := filepath.Join(dir, "export.json")
	if err := ExportDEK(dbPath, kek, recipient, exportPath); err != nil {
		t.Fatal(err)
	}

	// Move to a new location and import.
	newDir := t.TempDir()
	newDB := filepath.Join(newDir, "sessions.db")
	if err := os.Rename(dbPath+".enc", newDB+".enc"); err != nil {
		t.Fatal(err)
	}
	newKEK := makeKey()
	if err := ImportDEK(newDB, exportPath, recipient, newKEK); err != nil {
		t.Fatal(err)
	}

	// Open with the new KEK.
	s2, err := OpenWithKEK(newDB, newKEK)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.CloseEncrypted()
	got, _ := s2.EventsSince(context.Background(), sess, 0, 0)
	if len(got) != 1 || !strings.Contains(string(got[0].Payload), "exported") {
		t.Errorf("payload missing after export+import: %+v", got)
	}
}
