package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/storage"
)

func TestLocalStorage_DefaultFilesAreNotWorldReadable(t *testing.T) {
	requirePOSIXPermissions(t)
	t.Parallel()
	dir := t.TempDir()
	ls := storage.NewLocalStorage(dir)

	if err := ls.Save(context.Background(), "private/report.txt", strings.NewReader("secret")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "private", "report.txt"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if info.Mode().Perm()&0o044 != 0 {
		t.Fatalf("SECURITY: [storage-local] default saved file mode is too permissive: %o", info.Mode().Perm())
	}
}

func TestLocalStorage_DefaultDirectoriesAreNotWorldTraversable(t *testing.T) {
	requirePOSIXPermissions(t)
	t.Parallel()
	dir := t.TempDir()
	ls := storage.NewLocalStorage(dir)

	if err := ls.Save(context.Background(), "tenant-a/private/report.txt", strings.NewReader("secret")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "tenant-a", "private"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if info.Mode().Perm()&0o055 != 0 {
		t.Fatalf("SECURITY: [storage-local] default directory mode is too permissive: %o", info.Mode().Perm())
	}
}
