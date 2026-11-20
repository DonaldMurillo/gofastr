package verifycmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// TestVerifyCatalogOutputMatchesGolden pins every byte `gofastr verify`
// prints about the catalog: --list and --explain for each rule, in text
// and JSON. Regenerate with GOFASTR_UPDATE_GOLDEN=1 after an intended
// catalog change and read the diff.
func TestVerifyCatalogOutputMatchesGolden(t *testing.T) {
	// The binary run straight from a terminal (an IDE runner, go test -c)
	// would otherwise style the output and miss the plain golden.
	oldTTY := cli.StdoutIsTTY
	cli.StdoutIsTTY = false
	t.Cleanup(func() { cli.StdoutIsTTY = oldTTY })

	var b strings.Builder
	capture := func(args ...string) {
		out, code := captureVerify(t, args)
		if code != 0 {
			t.Fatalf("verify %v: exit = %d\n%s", args, code, out)
		}
		b.WriteString("=== gofastr verify " + strings.Join(args, " ") + "\n")
		b.WriteString(out)
		b.WriteString("\n")
	}
	capture("--list")
	capture("--list", "--json")
	for _, r := range contracts.AllRules() {
		capture("--explain", r.ID)
		capture("--explain", r.ID, "--json")
	}

	path := filepath.Join("testdata", "catalog.golden")
	got := b.String()
	if os.Getenv("GOFASTR_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading catalog golden: %v (run with GOFASTR_UPDATE_GOLDEN=1 to create)", err)
	}
	if got != string(want) {
		gotLines, wantLines := strings.Split(got, "\n"), strings.Split(string(want), "\n")
		for i := 0; i < len(gotLines) && i < len(wantLines); i++ {
			if gotLines[i] != wantLines[i] {
				t.Fatalf("catalog output drifted from %s at line %d:\n got: %q\nwant: %q", path, i+1, gotLines[i], wantLines[i])
			}
		}
		t.Fatalf("catalog output drifted from %s: %d lines, want %d", path, len(gotLines), len(wantLines))
	}
}
