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

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	return k
}

func TestEncryptedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.db")
	key := newKey(t)

	s, err := OpenEncrypted(path, EncryptionAtRest, key)
	if err != nil {
		t.Fatal(err)
	}
	sess := ids.NewSessionID()
	env, _ := control.EncodeEvent(1, control.TextDelta{Text: "secret-payload"}, sess, ids.NewClientID(), time.Now())
	if err := s.AppendEvent(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseEncrypted(); err != nil {
		t.Fatal(err)
	}

	// Plaintext file removed; .enc exists.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("plaintext file still present after CloseEncrypted")
	}
	if _, err := os.Stat(path + ".enc"); err != nil {
		t.Fatalf(".enc missing: %v", err)
	}

	// Reopen with the same key.
	s2, err := OpenEncrypted(path, EncryptionAtRest, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.CloseEncrypted()
	got, err := s2.EventsSince(context.Background(), sess, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(string(got[0].Payload), "secret-payload") {
		t.Errorf("payload not recovered: %+v", got)
	}
}

func TestEncryptedWrongKeyFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.db")
	key := newKey(t)

	s, _ := OpenEncrypted(path, EncryptionAtRest, key)
	env, _ := control.EncodeEvent(1, control.TextDelta{Text: "secret"}, ids.NewSessionID(), ids.NewClientID(), time.Now())
	_ = s.AppendEvent(context.Background(), env)
	_ = s.CloseEncrypted()

	wrong := newKey(t)
	_, err := OpenEncrypted(path, EncryptionAtRest, wrong)
	if err == nil {
		t.Fatal("expected decrypt failure with wrong key")
	}
}

func TestEncryptedRejectsBadKeyLength(t *testing.T) {
	dir := t.TempDir()
	_, err := OpenEncrypted(filepath.Join(dir, "x.db"), EncryptionAtRest, []byte("too-short"))
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestEncryptionNoneBehavesLikeOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.db")
	s, err := OpenEncrypted(path, EncryptionNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.CloseEncrypted()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created in plaintext mode: %v", err)
	}
}
