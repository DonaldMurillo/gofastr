package upload_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// On a case-insensitive filesystem "tenanta/report.txt" opens the file
// stored as "TenantA/report.txt". Save and Delete refused that spelling,
// but Get, GetRange and Exists read through it. Reads refuse it the same
// way, as battery/storage's local backend does.
func TestFoldedReadRefused(t *testing.T) {
	base := t.TempDir()
	ls := newTestLocalStorage(t, base)
	ctx := context.Background()
	if err := ls.Save(ctx, "TenantA/report.txt", strings.NewReader("tenant-a secret")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "tenanta", "report.txt")); err != nil {
		t.Skip("case-sensitive filesystem: tenanta/report.txt is another file")
	}
	key := "tenanta/report.txt"
	if rc, err := ls.Get(ctx, key); !errors.Is(err, upload.ErrInvalidKey) {
		got := ""
		if err == nil {
			b, _ := io.ReadAll(rc)
			rc.Close()
			got = string(b)
		}
		t.Errorf("Get(%q) = %q, %v; want ErrInvalidKey", key, got, err)
	}
	if f, err := ls.GetRange(ctx, key); !errors.Is(err, upload.ErrInvalidKey) {
		if err == nil {
			f.Close()
		}
		t.Errorf("GetRange(%q) err = %v, want ErrInvalidKey", key, err)
	}
	if ok, err := ls.Exists(ctx, key); !errors.Is(err, upload.ErrInvalidKey) {
		t.Errorf("Exists(%q) = %v, %v; want ErrInvalidKey", key, ok, err)
	}
	if ok, err := ls.Exists(ctx, "TenantA/report.txt"); err != nil || !ok {
		t.Errorf("Exists of the stored spelling = %v, %v; want true", ok, err)
	}
	if ok, err := ls.Exists(ctx, "TenantB/report.txt"); err != nil || ok {
		t.Errorf("Exists of an absent key = %v, %v; want false, nil", ok, err)
	}
}
