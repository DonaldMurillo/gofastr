package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// The local identity. A desktop app ships without battery/auth, but
// owner-scoped CRUD still needs an owner id. The LocalUser middleware
// sets a stable per-installation identity into the request context,
// and the owner extractor (installed only when nothing else installed
// one) reads it back. An app that also imports battery/auth keeps
// auth's extractor AND auth's identity: the local identity is a
// fallback for an app with no auth battery, never an override of one
// that has already decided, including a decision of "anonymous".
// See localUserMiddleware for why that distinction needed a field.

// identityFileName holds the per-installation random id.
const identityFileName = "identity"

// localUser is the context user value. Its GetID/GetEmail/GetRoles
// method set satisfies battery/auth's User interface structurally,
// without this package importing battery/auth.
type localUser struct {
	id string
}

func (u *localUser) GetID() string      { return u.id }
func (u *localUser) GetEmail() string   { return "local@" + u.id }
func (u *localUser) GetRoles() []string { return []string{"admin"} }

// readFileIfExists reads path, returning (nil, nil) when it does not
// exist yet.
func readFileIfExists(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("desktop: read %s: %w", path, err)
	}
	return b, nil
}

// identityIDLen is the minted id's length: 16 crypto/rand bytes as hex.
const identityIDLen = 32

// validIdentityID reports whether id matches the grammar
// loadOrMintLocalUser mints with. A value read back off disk and used
// as a security principal is re-validated against the grammar that
// minted it: the id is what every owner-scoped query compares against
// and what GetEmail interpolates, so a trailing newline or a truncated
// file would silently become a DIFFERENT owner and hide every row the
// app had written. That needs no attacker at all.
func validIdentityID(id string) bool {
	if len(id) != identityIDLen {
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

// errCorruptIdentity names the file and the repair, because the only
// safe repair is the user's call: minting a new id silently would orphan
// every row the old one owns.
func errCorruptIdentity(path string) error {
	return fmt.Errorf("desktop: identity file %s does not hold a %d-character hex id; "+
		"it is the owner id every stored row is scoped to, so this battery will not guess. "+
		"Restore the file, or delete it to mint a new identity (the old rows stay with the old id)",
		path, identityIDLen)
}

// loadOrMintLocalUser returns the identity stored under dir, minting
// (16 bytes of crypto/rand, hex) and persisting 0600 on first run.
func loadOrMintLocalUser(dir string) (*localUser, error) {
	path := filepath.Join(dir, identityFileName)
	if b, err := readFileIfExists(path); err != nil {
		return nil, err
	} else if len(b) > 0 {
		id := strings.TrimSpace(string(b))
		if !validIdentityID(id) {
			return nil, errCorruptIdentity(path)
		}
		return &localUser{id: id}, nil
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("desktop: mint local identity: %w", err)
	}
	id := hex.EncodeToString(raw[:])
	if err := writeFileExclusive(path, []byte(id)); err != nil {
		if os.IsExist(err) {
			// Lost the race: another process minted it first. Its bytes go
			// through the same grammar check as any other read.
			if b, rerr := readFileIfExists(path); rerr == nil && len(b) > 0 {
				raced := strings.TrimSpace(string(b))
				if !validIdentityID(raced) {
					return nil, errCorruptIdentity(path)
				}
				return &localUser{id: raced}, nil
			}
		}
		return nil, fmt.Errorf("desktop: write identity file: %w", err)
	}
	return &localUser{id: id}, nil
}

// localUserMiddleware installs the identity into the request context.
// It applies ONLY while the boot gate is armed, which Run does before
// the listener opens: a request that reached the handler then came
// from the desktop window and carries the boot cookie, so it is the
// local user. An app serving itself over plain HTTP (the --serve
// mode; the gate stays unarmed) gets no identity from this battery,
// because every anonymous browser would otherwise become the local
// admin. Such an app brings battery/auth or stays anonymous.
//
// Within desktop mode it is a FALLBACK, and the decision about whether
// it applies at all is made at INIT, not per request.
//
// The per-request test used to be `if _, ok := handler.GetUser(ctx);
// !ok`, which looks like "nobody has decided yet" and is not. When
// battery/auth is mounted, its session middleware marks an anonymous
// request with handler.SetUser(ctx, nil), and handler.GetUser type-
// asserts, so a stored nil reads back ok=FALSE. Auth's explicit "nobody
// is signed in" was byte-identical to "no middleware ran", and this
// fallback overrode it, handing a signed-out desktop window the local
// identity, whose GetRoles() is ["admin"], which is exactly the
// structural interface battery/admin authorizes through. Worse, which
// way it went depended on battery REGISTRATION ORDER: with desktop's
// Use installed first, auth's anon path overwrote the local user with
// nil instead.
//
// So: battery/auth installs the framework/owner extractor from its
// package init(), which makes owner.GetExtractor() != nil a reliable
// "an auth battery is linked into this binary" signal, the same test
// installOwnerExtractor already makes. When it is set, this battery
// installs NO identity and auth owns identity end to end. An app that
// wants an identity in --serve mode brings battery/auth or stays
// anonymous.
func (b *Battery) localUserMiddleware() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !b.gateArmed.Load() || b.authOwnsIdentity {
				next.ServeHTTP(w, r)
				return
			}
			if _, ok := handler.GetUser(r.Context()); !ok {
				r = r.WithContext(handler.SetUser(r.Context(), b.user))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// installOwnerExtractor sets the process-wide owner extractor only when
// none exists, and records whether one already did. battery/auth
// installs its own from init; never overwrite it, and never speak for a
// request it has already decided about (see localUserMiddleware).
func (b *Battery) installOwnerExtractor() {
	if cur := owner.GetExtractor(); cur != nil {
		// An extractor is installed. It is either battery/auth's (from
		// its package init, before any battery's Init) or the one a
		// desktop battery installed earlier in this process: every Go
		// test suite builds several apps per process, and taking the
		// first battery's extractor for auth's turned every later
		// window anonymous (POST from the window: 401).
		if !isDesktopExtractor(cur) {
			b.authOwnsIdentity = true
			return
		}
		b.authOwnsIdentity = false
		return
	}
	b.authOwnsIdentity = false
	owner.SetExtractor(desktopOwnerExtractor)
}

// desktopOwnerExtractor reads the owner id off the context user the
// local-identity middleware (or any other identity source) set. It is
// a named top-level function so a later battery can recognize it.
func desktopOwnerExtractor(ctx context.Context) (any, bool) {
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

// isDesktopExtractor reports whether fn is desktopOwnerExtractor. Func
// values are not comparable; their code pointers are, and a top-level
// function has exactly one.
func isDesktopExtractor(fn owner.Extractor) bool {
	return reflect.ValueOf(fn).Pointer() == reflect.ValueOf(owner.Extractor(desktopOwnerExtractor)).Pointer()
}
