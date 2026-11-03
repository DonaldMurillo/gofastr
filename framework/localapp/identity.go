// Package localapp is the local state of an app that runs for one OS user
// on one device, with no server of its own to sign people in: a desktop
// window (battery/desktop), a phone app serving itself in process
// (gofastr-mobile), a signed-in service on the user's machine. [Open]
// gives such an app its data directory's contents: the SQLite database,
// the session secret that keeps sessions valid across restarts, and the
// installation's identity.
//
// The identity exists because owner-scoped CRUD still needs an owner id.
// The host puts a stable per-installation [User] into the request
// context, and the owner extractor that [Install] sets up reads it
// back.
//
// The identity is a FALLBACK for an app with no auth battery, never an
// override of one. battery/auth installs the framework/owner extractor
// from its package init, so a foreign extractor already in place is a
// reliable "auth owns identity" signal, and Install reports it. A host
// that sees it installs no identity at all: auth's "this request is
// anonymous" is a decision, and handler.GetUser cannot tell a stored nil
// from no decision (the 2026-09-04 desktop finding pinned in
// battery/desktop/localuser_security_test.go).
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

// idLen is a minted id's length: 16 crypto/rand bytes as hex.
const idLen = 32

// User is the context user value. Its GetID/GetEmail/GetRoles method
// set satisfies battery/auth's User interface structurally, without
// importing battery/auth.
type User struct {
	id string
}

// ForID returns the User for id, for tests and for a battery that
// already holds a validated id.
func ForID(id string) *User { return &User{id: id} }

func (u *User) GetID() string    { return u.id }
func (u *User) GetEmail() string { return "local@" + u.id }

// GetRoles is ["admin"]: the one person who can reach a single-user
// app is its owner. Install bounds WHEN this identity is handed out.
func (u *User) GetRoles() []string { return []string{"admin"} }

// Valid reports whether id matches the grammar LoadOrMint mints with.
// A value read back off disk and used as a security principal is
// re-validated: it is what every owner-scoped query compares against,
// so a trailing newline or a truncated file would silently become a
// different owner and hide every row the app had written.
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

// errCorrupt names the file and the repair, because the only safe
// repair is the user's call: minting a new id silently would orphan
// every row the old one owns.
func errCorrupt(path string) error {
	return fmt.Errorf("local identity file %s does not hold a %d-character hex id; "+
		"it is the owner id every stored row is scoped to, so it will not be guessed. "+
		"Restore the file, or delete it to mint a new identity (the old rows stay with the old id)",
		path, idLen)
}

// LoadOrMint returns the identity stored under dir, minting one (16
// bytes of crypto/rand, hex) and persisting it 0600 on first run.
func LoadOrMint(dir string) (*User, error) {
	path := filepath.Join(dir, IdentityFile)
	if b, err := readIfExists(path); err != nil {
		return nil, err
	} else if len(b) > 0 {
		id := strings.TrimSpace(string(b))
		if !Valid(id) {
			return nil, errCorrupt(path)
		}
		return &User{id: id}, nil
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
			if b, rerr := readIfExists(path); rerr == nil && len(b) > 0 {
				raced := strings.TrimSpace(string(b))
				if !Valid(raced) {
					return nil, errCorrupt(path)
				}
				return &User{id: raced}, nil
			}
		}
		return nil, fmt.Errorf("write local identity file: %w", err)
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

// readOwnerOnly is readIfExists for a secret: a file another user can
// read (a restored backup, a copy made with a broad umask) is tightened
// to owner-only through the open handle before it is read.
func readOwnerOnly(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close()
	if err := fileperm.TightenOpenFile(f); err != nil {
		return nil, fmt.Errorf("restrict %s to its owner: %w", path, err)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxStateFile))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return b, nil
}

// maxStateFile bounds a read of the secret: the minted value is 43
// bytes, so anything near this is damaged, and checkSecret refuses it.
const maxStateFile = 4 << 10

// writeExclusive publishes b at path as an owner-only file, refusing
// an existing one. The publish is atomic: a process racing the first
// boot reads no file or the whole value, never an empty file.
func writeExclusive(path string, b []byte) error {
	return fileperm.PublishNewOwnerOnly(path, b)
}

// Install sets Extractor as the process-wide owner extractor when no
// extractor is set, and reports whether a foreign one (battery/auth's,
// from its package init) owns identity. Extractor itself, installed
// earlier by another battery or another App in the same process, is
// not foreign: every Go test suite builds several apps per process.
func Install() (authOwnsIdentity bool) {
	cur := owner.GetExtractor()
	if cur == nil {
		owner.SetExtractor(Extractor)
		return false
	}
	return !IsExtractor(cur)
}

// Extractor reads the owner id off the context user. It is a named
// top-level function so IsExtractor can recognize it.
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

// IsExtractor reports whether fn is Extractor. Func values are not
// comparable; their code pointers are, and a top-level function has
// exactly one.
func IsExtractor(fn owner.Extractor) bool {
	return reflect.ValueOf(fn).Pointer() == reflect.ValueOf(owner.Extractor(Extractor)).Pointer()
}
