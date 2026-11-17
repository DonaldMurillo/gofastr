//go:build windows

package localrun

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

const openRecordFlags = os.O_RDONLY

func checkDir(dir string) (os.FileInfo, error) {
	fi, err := os.Lstat(dir)
	if err != nil {
		return nil, fmt.Errorf("localrun: %w", err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("localrun: %s is not a directory", dir)
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return nil, fmt.Errorf("localrun: read owner of %s: %w", dir, err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return nil, fmt.Errorf("localrun: read owner of %s: %w", dir, err)
	}
	tok := windows.GetCurrentProcessToken()
	user, err := tok.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("localrun: %w", err)
	}
	def, err := tokenDefaultOwner(tok)
	if err != nil {
		return nil, fmt.Errorf("localrun: %w", err)
	}
	if err := checkOwner(dir, owner.String(), user.User.Sid.String(), def); err != nil {
		return nil, err
	}
	return fi, nil
}

func ownFile(f *os.File, _ os.FileInfo) bool {
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return false
	}
	tok := windows.GetCurrentProcessToken()
	user, err := tok.GetTokenUser()
	if err != nil {
		return false
	}
	def, _ := tokenDefaultOwner(tok)
	return checkOwner("", owner.String(), user.User.Sid.String(), def) == nil
}

func tokenDefaultOwner(tok windows.Token) (string, error) {
	var n uint32
	_ = windows.GetTokenInformation(tok, windows.TokenOwner, nil, 0, &n)
	if n == 0 {
		return "", fmt.Errorf("token owner: empty")
	}
	buf := make([]byte, n)
	if err := windows.GetTokenInformation(tok, windows.TokenOwner, &buf[0], n, &n); err != nil {
		return "", fmt.Errorf("token owner: %w", err)
	}
	return (*struct{ Owner *windows.SID })(unsafe.Pointer(&buf[0])).Owner.String(), nil
}

func secureOpened(_ *os.Root, _ os.FileInfo, dir string) error {
	if err := fileperm.Restrict(dir, true); err != nil {
		return fmt.Errorf("localrun: %w", err)
	}
	return nil
}
