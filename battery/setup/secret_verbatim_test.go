package setup

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
)

// A password is the bytes the operator typed. The wizard trimmed every
// field, so an admin password with a space at either end was stored
// without it, and the sign-in form, which compares the bytes it is
// sent, refused the password the operator had just chosen. The headless
// path never trimmed, so the same value from the environment worked.
func TestWizardStoresSecretAsTyped(t *testing.T) {
	mgr, db := setupAuthManager(t)
	step, complete := AdminStep(mgr, db, "auth_users")
	r := New(Config{DisableToken: true, Complete: complete, Steps: []Step{step}})
	h := r.Handler(func() {}, nil, nil)

	const typed = " correct horse "
	form := url.Values{"ADMIN_EMAIL": {" admin@example.com "}, "ADMIN_PASSWORD": {typed}}
	if w := doPost(h, "/setup", form.Encode(), nil); w.Code != http.StatusOK {
		t.Fatalf("POST /setup: got %d, want the completion page", w.Code)
	}

	// The address is still trimmed: it is a name, not a secret.
	_, hash, err := mgr.UserStore().FindByEmail(context.Background(), "admin@example.com")
	if err != nil {
		t.Fatalf("the admin was not created under the trimmed address: %v", err)
	}
	if !auth.CheckPassword(typed, hash) {
		t.Fatal("the password the operator typed does not sign in: the wizard changed it before hashing")
	}
}

// A secret is validated as typed too: eight characters that begin with a
// space are eight characters.
func TestWizardValidatesSecretAsTyped(t *testing.T) {
	var got string
	done := false
	r := New(Config{
		DisableToken: true,
		Complete:     func(context.Context) (bool, error) { return done, nil },
		Steps: []Step{{Name: "s", Fields: []Field{
			{Name: "KEY", Secret: true, Validate: func(v string) error { got = v; return nil }},
		}, Run: func(context.Context, map[string]string) error { done = true; return nil }}},
	})
	h := r.Handler(func() {}, nil, nil)
	doPost(h, "/setup", url.Values{"KEY": {"  abc  "}}.Encode(), nil)
	if got != "  abc  " {
		t.Fatalf("Validate saw %q, want the value as typed", got)
	}
}
