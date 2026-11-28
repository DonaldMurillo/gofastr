package exportcmd

import (
	"os"
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
