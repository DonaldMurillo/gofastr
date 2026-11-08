package contracts

import (
	"bytes"
	"github.com/DonaldMurillo/gofastr/internal/atomicfile"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBaselineReadersKeepWholeSnapshot(t *testing.T) {
	p := filepath.Join(t.TempDir(), BaselineFileName)
	for _, size := range []int{64, 128 * 1024} {
		old := &Baseline{Schema: BaselineSchemaVersion, Note: strings.Repeat("old", size), Counts: map[string]map[string]int{}}
		if err := WriteBaseline(p, old); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := atomicfile.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		next := &Baseline{Schema: BaselineSchemaVersion, Note: "replacement", Counts: map[string]map[string]int{}}
		if err := WriteBaseline(p, next); err != nil {
			reader.Close()
			t.Fatal(err)
		}
		got, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, before) {
			t.Fatalf("open reader lost its complete snapshot (old=%d, got=%d)", len(before), len(got))
		}
		current, err := ReadBaseline(p)
		if err != nil || current.Note != next.Note {
			t.Fatalf("current=%v err=%v", current, err)
		}
	}
}
