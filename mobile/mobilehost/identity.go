package mobilehost

import (
	"context"
	"net/http"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// PROTOTYPE FINDING: this is a second copy of internal/localuser (the
// identity battery/desktop and battery/service share). Beyond the drift
// risk, two copies break localuser.Install's "is the installed extractor
// a foreign one (battery/auth)?" test: it recognizes its OWN extractor by
// code pointer, so it reads this package's extractor as auth's and would
// withdraw the desktop identity in a process that links both. A split
// needs localuser public.

// localUser is the single person who can reach an in-process app: the
// owner of the phone the app runs on. Its method set matches battery/
// auth's User interface structurally.
type localUser struct{ id string }

func (u *localUser) GetID() string      { return u.id }
func (u *localUser) GetEmail() string   { return "local@" + u.id }
func (u *localUser) GetRoles() []string { return []string{"admin"} }

func loadIdentity(dataDir string) (*localUser, error) {
	id, err := loadOrMint(filepath.Join(dataDir, "identity"), validHexID)
	if err != nil {
		return nil, err
	}
	return &localUser{id: id}, nil
}

// extractor reads the owner id off the context user.
func extractor(ctx context.Context) (any, bool) {
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

// installExtractor sets the owner extractor when none is set and reports
// whether a foreign one (battery/auth's, installed from its package init)
// owns identity, in which case the host installs no identity at all.
func installExtractor() (authOwnsIdentity bool) {
	if owner.GetExtractor() == nil {
		owner.SetExtractor(extractor)
		return false
	}
	return true
}

// identityMiddleware hands every request the local identity. Unlike the
// desktop shell's, it needs no boot gate: only the in-process transport
// can reach the server (there is no socket), so every request that
// arrives came from this app's WebView.
func (h *Host) identityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.authOwnsIdentity {
			if _, ok := handler.GetUser(r.Context()); !ok {
				r = r.WithContext(handler.SetUser(r.Context(), h.user))
			}
		}
		next.ServeHTTP(w, r)
	})
}
