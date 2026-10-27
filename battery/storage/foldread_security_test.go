package storage

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
// but Get, GetRange and Exists read through it, so a key another tenant
// never stored returned that tenant's bytes. Reads refuse it the same way.
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
	for _, key := range []string{"tenanta/report.txt", "TenantA/REPORT.txt"} {
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
	}
	rc, err := ls.Get(ctx, "TenantA/report.txt")
	if err != nil {
		t.Fatalf("Get of the stored spelling: %v", err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "tenant-a secret" {
		t.Errorf("stored spelling read %q", b)
	}
	if ok, err := ls.Exists(ctx, "TenantA/report.txt"); err != nil || !ok {
		t.Errorf("Exists of the stored spelling = %v, %v; want true", ok, err)
	}
	if ok, err := ls.Exists(ctx, "TenantB/report.txt"); err != nil || ok {
		t.Errorf("Exists of an absent key = %v, %v; want false, nil", ok, err)
	}
}
