package nowaitdelay_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	fixture "github.com/DonaldMurillo/gofastr/internal/analyzers/nowaitdelay/testdata/src/bounded"
)

func TestBoundLateArgsStartsChild(t *testing.T) {
	if os.Getenv("GOFASTR_BOUND_CHILD") == "1" {
		if err := os.WriteFile(os.Getenv("GOFASTR_BOUND_CHILD_MARKER"), []byte("started"), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Setenv("GOFASTR_BOUND_CHILD", "1")
	marker := filepath.Join(t.TempDir(), "started")
	t.Setenv("GOFASTR_BOUND_CHILD_MARKER", marker)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := fixture.BoundLateArgs(ctx, os.Args[0]); err != nil {
		t.Fatalf("bounded child did not run: %v", err)
	}
	if contents, err := os.ReadFile(marker); err != nil || string(contents) != "started" {
		t.Fatalf("bounded child never wrote its startup marker: %q, %v", contents, err)
	}
}
