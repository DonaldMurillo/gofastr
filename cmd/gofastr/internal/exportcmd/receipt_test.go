package exportcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestExportRefusesAReceiptForAnotherRun(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs an app")
	}
	dir := t.TempDir()
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() {
			RunExport([]string{"--pkg", "./testdata/otherrunapp", dir})
		})
	})
	if code != 1 || strings.Contains(out, "Exported") {
		t.Fatalf("a receipt naming another run was accepted: exit %d\n%s", code, out)
	}
}

func TestClearReceiptRemovesAnEarlierRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, framework.ExportReceiptFile)
	if err := os.WriteFile(path, []byte(`{"run":"old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := clearReceipt(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("an earlier run's receipt survived: %v", err)
	}
	if err := clearReceipt(dir); err != nil {
		t.Fatalf("no receipt to clear is not an error: %v", err)
	}
}

func TestReadReceiptIsBounded(t *testing.T) {
	dir := t.TempDir()
	body := `{"run":"` + strings.Repeat("a", maxReceiptBytes) + `","pages":1}`
	if err := os.WriteFile(filepath.Join(dir, framework.ExportReceiptFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := readReceipt(dir); err == nil {
		t.Fatal("a receipt larger than maxReceiptBytes was read whole")
	}
}

// fakeExport runs RunExport with app in place of the app's main, given
// the export directory and run id RunExport passed, and returns the exit
// code and combined output.
func fakeExport(t *testing.T, dir string, app func(dir, run string)) (int, string) {
	t.Helper()
	old := runApp
	t.Cleanup(func() { runApp = old })
	runApp = func(cmd *exec.Cmd) error {
		env := map[string]string{}
		for _, kv := range cmd.Env {
			if k, v, ok := strings.Cut(kv, "="); ok {
				env[k] = v
			}
		}
		app(env["GOFASTR_EXPORT"], env["GOFASTR_EXPORT_RUN"])
		return nil
	}
	var code int
	out := clitest.CovT_capOutput(t, func() {
		code = clitest.CovT_capExit(t, func() {
			RunExport([]string{"--pkg", "./testdata/noexportapp", dir})
		})
	})
	return code, out
}

func writeReceipt(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, framework.ExportReceiptFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestExportClearsAnEarlierReceiptBeforeTheAppRuns(t *testing.T) {
	dir := t.TempDir()
	writeReceipt(t, dir, `{"run":"old"}`)
	code, out := fakeExport(t, dir, func(dir, _ string) {
		if _, err := os.Stat(filepath.Join(dir, framework.ExportReceiptFile)); !os.IsNotExist(err) {
			t.Errorf("the earlier receipt was still there when the app started: %v", err)
		}
	})
	if code != 1 || !strings.Contains(out, "exited 0 but exported nothing") {
		t.Fatalf("an app that wrote no receipt after an earlier run: exit %d\n%s", code, out)
	}
}

func TestExportChecksTheRunTheReceiptNames(t *testing.T) {
	t.Run("another run", func(t *testing.T) {
		code, out := fakeExport(t, t.TempDir(), func(dir, _ string) {
			writeReceipt(t, dir, `{"run":"another-run","pages":3}`)
		})
		if code != 1 || !strings.Contains(out, "names another run") || strings.Contains(out, "Exported") {
			t.Fatalf("a receipt naming another run was accepted: exit %d\n%s", code, out)
		}
	})
	t.Run("this run", func(t *testing.T) {
		code, out := fakeExport(t, t.TempDir(), func(dir, run string) {
			writeReceipt(t, dir, `{"run":"`+run+`","pages":3}`)
		})
		if code != -1 || !strings.Contains(out, "Exported 3 page(s)") {
			t.Fatalf("the receipt for this run was refused: exit %d\n%s", code, out)
		}
	})
}
