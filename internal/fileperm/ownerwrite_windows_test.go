package fileperm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
	"golang.org/x/sys/windows"
)

func TestOwnerWritesProtectWindowsACL(t *testing.T) {
	for _, seed := range []bool{false, true} {
		for _, existing := range []bool{false, true} {
			path := filepath.Join(t.TempDir(), "secret")
			if existing {
				if err := os.WriteFile(path, []byte("old"), 0o666); err != nil {
					t.Fatal(err)
				}
			}
			var err error
			if seed {
				err = SeedOwnerOnly(path)
			} else {
				err = WriteOwnerOnly(path, []byte("new"))
			}
			if err != nil {
				t.Fatal(err)
			}
			filepermtest.AssertOwnerOnly(t, path, 0o600)
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := "new"
			if seed {
				want = ""
				if existing {
					want = "old"
				}
			}
			if string(body) != want {
				t.Fatalf("content = %q, want %q", body, want)
			}
		}
	}
}

func TestRestrictOpenFileKeepsObjectIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(wide, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	f := os.NewFile(uintptr(handle), path)
	defer f.Close()
	moved := path + ".moved"
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := RestrictOpenFile(f); err != nil {
		t.Fatal(err)
	}
	filepermtest.AssertOwnerOnly(t, moved, 0o600)
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED != 0 {
		t.Fatalf("restriction followed the stale filename: control=%#x, error=%v", control, err)
	}
}
