package exportcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestExportOptions(t *testing.T) {
	opts, err := parseExportOptions([]string{"--pkg", "./cmd/web", "--base=/docs", "out"})
	if err != nil || opts != (exportOptions{pkg: "./cmd/web", base: "/docs", dir: "out"}) {
		t.Fatalf("opts = %+v, err %v", opts, err)
	}
	for _, args := range [][]string{
		{},                         // no directory
		{"a", "b"},                 // two directories
		{"--base", "docs", "out"},  // relative base
		{"--base=/docs/", "out"},   // trailing slash
		{"--pkg", "--base", "out"}, // flag as a value
		{"--force", "out"},
	} {
		if _, err := parseExportOptions(args); err == nil {
			t.Errorf("%q parsed", args)
		}
	}
}

// TestExportRunsTheApp runs `gofastr export` against a fixture app and
// reads the site it wrote: both pages under the base path and the
// search battery's index beside them.
func TestExportRunsTheApp(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs an app")
	}
	dir := t.TempDir()
	RunExport([]string{"--pkg", "./testdata/exportapp", "--base", "/sub", dir})
	about, err := os.ReadFile(filepath.Join(dir, "about", "index.html"))
	if err != nil {
		t.Fatalf("about page missing: %v", err)
	}
	if !strings.Contains(string(about), "Wombats") || !strings.Contains(string(about), `"/sub/__gofastr/`) {
		t.Errorf("about page = %.300s", about)
	}
	if _, err := os.Stat(filepath.Join(dir, "__gofastr", "search", "manifest.json")); err != nil {
		t.Errorf("search index missing: %v", err)
	}
	// The receipt is the CLI's to read, not part of the site it ships.
	if _, err := os.Stat(filepath.Join(dir, framework.ExportReceiptFile)); !os.IsNotExist(err) {
		t.Errorf("the export receipt was left in the site: %v", err)
	}
}

// A main that exits 0 without reaching App.Start exported nothing, and
// `gofastr export` reported "Exported to <dir>" over an empty or stale
// directory. The app writes a receipt for the run the CLI names; with
// none, the export failed.
func TestExportFailsWithoutReceipt(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs an app")
	}
	dir := t.TempDir()
	// A stale receipt from an earlier run must not count for this one.
	if err := os.WriteFile(filepath.Join(dir, framework.ExportReceiptFile), []byte(`{"run":"old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() {
			RunExport([]string{"--pkg", "./testdata/noexportapp", dir})
		})
	})
	if code != 1 {
		t.Fatalf("export of an app that never exported exited %d, want 1\n%s", code, out)
	}
	if strings.Contains(out, "Exported to") {
		t.Errorf("reported success:\n%s", out)
	}
}

// The receipt is required only when the app's framework writes one:
// this repository's own packages, and a go.mod pinning the release
// that added it or later. An older pin, or a replace directive whose
// source version is unknown, keeps the exit status as the only signal.
func TestExportReceiptRequired(t *testing.T) {
	gomod := func(t *testing.T, body string) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	const req = "module example.com/app\n\ngo 1.24\n\nrequire github.com/DonaldMurillo/gofastr "
	for _, tc := range []struct {
		name, gomod string
		want        receiptPolicy
	}{
		{"old pin", req + "v0.95.0\n", receiptUnknown},
		{"pseudo before the release", req + "v0.95.1-0.20261101000000-abcdefabcdef\n", receiptUnknown},
		{"the release", req + "v0.96.0\n", receiptRequired},
		{"later", req + "v0.97.2\n", receiptRequired},
		{"replaced", req + "v0.96.0\n\nreplace github.com/DonaldMurillo/gofastr => ../gofastr\n", receiptUnknown},
		{"no gofastr", "module example.com/app\n\ngo 1.24\n", receiptUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := exportReceiptPolicy(gomod(t, tc.gomod)); got != tc.want {
				t.Errorf("policy = %v, want %v", got, tc.want)
			}
		})
	}
	if got := exportReceiptPolicy("./testdata/noexportapp"); got != receiptRequired {
		t.Errorf("a package inside this repository: policy = %v, want required", got)
	}
}
