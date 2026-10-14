package localuser

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

func keepExtractor(t *testing.T) {
	t.Helper()
	prev := owner.GetExtractor()
	t.Cleanup(func() { owner.SetExtractor(prev) })
}

func TestInstallWhenAbsent(t *testing.T) {
	keepExtractor(t)
	owner.SetExtractor(nil)
	if Install() {
		t.Fatal("reported auth ownership with no extractor set")
	}
	id, ok := owner.Get(handler.SetUser(context.Background(), ForID("abc")))
	if !ok || id != "abc" {
		t.Fatalf("owner.Get = (%v, %v); want (abc, true)", id, ok)
	}
}

// A second battery, or a second App in the same process, finds this
// package's extractor and must not read it as battery/auth's.
func TestInstallOwnExtractorIsNotForeign(t *testing.T) {
	keepExtractor(t)
	owner.SetExtractor(Extractor)
	if Install() {
		t.Fatal("this package's own extractor read as an auth battery's")
	}
}

func TestInstallForeignExtractorOwnsIdentity(t *testing.T) {
	keepExtractor(t)
	owner.SetExtractor(func(context.Context) (any, bool) { return "auth", true })
	if !Install() {
		t.Fatal("a foreign extractor did not report auth ownership")
	}
	if id, _ := owner.Get(context.Background()); id != "auth" {
		t.Fatalf("the foreign extractor was replaced: owner.Get = %v", id)
	}
}

func TestLoadOrMintStableOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	a, err := LoadOrMint(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadOrMint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.GetID() != b.GetID() || !Valid(a.GetID()) {
		t.Fatalf("minted %q then read %q", a.GetID(), b.GetID())
	}
	fi, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("identity file mode %v; want owner-only", fi.Mode().Perm())
	}
}

func TestLoadOrMintRefusesCorruptFile(t *testing.T) {
	for _, content := range []string{"short", "0123456789abcdef0123456789ABCDEF", "0123456789abcdef0123456789abcdef00"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if u, err := LoadOrMint(dir); err == nil {
			t.Errorf("identity file %q became owner %q", content, u.GetID())
		}
	}
}
