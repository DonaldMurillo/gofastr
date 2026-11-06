package service

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
)

func TestTrimLogBoundsArchivedTail(t *testing.T) {
	for _, multiple := range []int64{2, 5} {
		path := filepath.Join(shortTempDir(t), "service.log")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		size := multiple*maxLogSize + 73
		expected := bytes.Repeat([]byte("recent log\n"), maxLogSize/11+1)[:maxLogSize]
		if err := file.Truncate(size); err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteAt(expected, size-maxLogSize); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if err := trimLog(path); err != nil {
			t.Fatal(err)
		}
		archived, err := os.ReadFile(path + ".1")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(archived, expected) {
			t.Fatalf("%d-byte log archived %d bytes, want exactly its last %d", size, len(archived), maxLogSize)
		}
		filepermtest.AssertOwnerOnly(t, path+".1", 0o600)
		current, err := os.Stat(path)
		if err != nil || current.Size() != 0 {
			t.Fatalf("current log not truncated: %v", err)
		}
	}
}
