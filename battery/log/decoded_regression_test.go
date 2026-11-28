package log

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTailWindowStartsAtCompleteLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	const first = "{\"id\":1}\n"
	const last = "{\"id\":2}\n"
	if err := os.WriteFile(path, []byte(first+last), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := tailFileWithWindow(path, 1, int64(len(last)))
	if err != nil || len(got) != 1 {
		t.Fatalf("tail = %v, %v; want the complete last line", got, err)
	}
}

func TestReadLineCappedRejectsOverlongUnterminatedLine(t *testing.T) {
	line, err := readLineCapped(bufio.NewReader(strings.NewReader("12345")), 4)
	if line != nil || !errors.Is(err, io.EOF) {
		t.Fatalf("line = %q, %v; want nil, EOF", line, err)
	}
}

func TestDecodedLogsSkipNullEntries(t *testing.T) {
	const data = "null\n{\"id\":1}\n"
	path := filepath.Join(t.TempDir(), "log.jsonl")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := tailFile(path, 2)
	if err != nil || len(got) != 1 || got[0] == nil {
		t.Errorf("tail = %v, %v; want only the record", got, err)
	}
	ring := NewRingSink(2)
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		if err := ring.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if got := ring.SnapshotDecoded(); len(got) != 1 || got[0] == nil {
		t.Fatalf("snapshot = %v; want only the record", got)
	}
}
