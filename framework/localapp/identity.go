// Package localapp is the local state of a one-user, one-device app ([Open], [Install]).
package localapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// IdentityFile is the identity file inside the app's data directory.
const IdentityFile = "identity"

const idLen = 32

// User is the installation's context user (battery/auth's User, structurally).
type User struct {
	id string
}

// ForID returns the User for an already validated id.
func ForID(id string) *User { return &User{id: id} }

func (u *User) GetID() string    { return u.id }
func (u *User) GetEmail() string { return "local@" + u.id }

func (u *User) GetRoles() []string { return []string{"admin"} }

// Valid reports whether id is a 32-character lowercase hex id.
func Valid(id string) bool {
	if len(id) != idLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func errCorrupt(path string) error {
	return fmt.Errorf("local identity file %s does not hold a %d-character hex id; "+
		"it is the owner id every stored row is scoped to, so it will not be guessed. "+
		"Restore the file, or delete it to mint a new identity (the old rows stay with the old id)",
		path, idLen)
}

// LoadOrMint returns the identity stored under dir, minting a 0600 one first.
func LoadOrMint(dir string) (*User, error) {
	path := filepath.Join(dir, IdentityFile)
	if b, err := readIfExists(path); err != nil {
		return nil, err
	} else if b != nil {
		return parseIdentity(path, b)
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("mint local identity: %w", err)
	}
	id := hex.EncodeToString(raw[:])
	if err := writeExclusive(path, []byte(id)); err != nil {
		if os.IsExist(err) {
			// Lost the race: another process minted it first. Its bytes
			// go through the same grammar check as any other read.
			if b, rerr := readIfExists(path); rerr == nil && b != nil {
				return parseIdentity(path, b)
			}
		}
		return nil, fmt.Errorf("write local identity file: %w", err)
	}
	return &User{id: id}, nil
}

func parseIdentity(path string, b []byte) (*User, error) {
	id := strings.TrimSpace(string(b))
	if !Valid(id) {
		return nil, errCorrupt(path)
	}
	return &User{id: id}, nil
}

func readIfExists(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return b, nil
}

func readOwnerOnly(path string) ([]byte, error) {
	lst, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if !lst.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file (a symlink?); replace it with the file itself", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close()
	if fst, err := f.Stat(); err != nil || !os.SameFile(lst, fst) {
		return nil, fmt.Errorf("%s changed while it was opened", path)
	}
	if err := fileperm.TightenOpenFile(f); err != nil {
		return nil, fmt.Errorf("restrict %s to its owner: %w", path, err)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxStateFile))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return b, nil
}

const maxStateFile = 4 << 10

func writeExclusive(path string, b []byte) error {
	return fileperm.PublishNewOwnerOnly(path, b)
}

// Install sets Extractor when no owner extractor is set; true means auth's is.
func Install() (authOwnsIdentity bool) {
	cur := owner.GetExtractor()
	if cur == nil {
		owner.SetExtractor(Extractor)
		return false
	}
	return !IsExtractor(cur)
}

// Extractor reads the owner id off the context user.
func Extractor(ctx context.Context) (any, bool) {
	v, ok := handler.GetUser(ctx)
	if !ok {
		return nil, false
	}
	type ider interface{ GetID() string }
	if u, ok := v.(ider); ok {
		return u.GetID(), true
	}
	return nil, false
}

// IsExtractor reports whether fn is Extractor, by code pointer.
func IsExtractor(fn owner.Extractor) bool {
	return reflect.ValueOf(fn).Pointer() == reflect.ValueOf(owner.Extractor(Extractor)).Pointer()
}
