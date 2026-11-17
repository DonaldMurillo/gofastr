package axecov

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/importcheck"
)

func TestSweepSchemesAreSorted(t *testing.T) {
	dir := t.TempDir()
	for _, s := range []string{"light", "dark"} {
		if err := Record(dir, "/", s); err != nil {
			t.Fatal(err)
		}
	}
	m, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Pages["/"]; !slices.Equal(got, []string{"dark", "light"}) {
		t.Errorf("schemes = %v, want [dark light]", got)
	}
}

func sweepWriteManifest(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".gofastr"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSweepRecordRefusesToOverwriteACorruptManifest(t *testing.T) {
	dir := sweepWriteManifest(t, "{corrupt")
	if err := Record(dir, "/", "dark"); err == nil {
		t.Fatal("Record replaced a corrupt manifest instead of reporting it")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, FileName)); string(b) != "{corrupt" {
		t.Errorf("corrupt manifest was rewritten: %q", b)
	}
}

func TestSweepRecordIntoAManifestWithoutPages(t *testing.T) {
	dir := sweepWriteManifest(t, `{"version":1}`)
	if err := Record(dir, "/a", "dark"); err != nil {
		t.Fatal(err)
	}
	if m, err := Read(dir); err != nil || len(m.Pages["/a"]) != 1 {
		t.Errorf("Read = %v, %v; want /a recorded", m, err)
	}
}

func TestSweepAxecovLinksNoBrowser(t *testing.T) {
	importcheck.Run(t, importcheck.Ban{
		Deny:      "github.com/chromedp/chromedp",
		Primitive: "testkit.axe.coverage",
		Why:       "uihost strict mode reads the manifest in every app, which must not link a headless browser driver.",
		Instead:   "Keep browser code in framework/testkit/axetest.",
	})
}
