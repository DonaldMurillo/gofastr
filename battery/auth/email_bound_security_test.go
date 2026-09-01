package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Property: an unauthenticated endpoint that accepts an email address it
// persists or transmits must enforce the battery's maxEmailLen (254, RFC
// 5321) bound at ingestion. decodeAuthCredentials applies exactly this
// rule to login/register (form_decode.go:83-101) with the rationale that
// without it "maxAuthBodyBytes (1 MiB) is the only limit on how much
// state one unauthenticated request can park" in the tables behind those
// handlers. The email-issuing endpoints take the same unauthenticated
// input and do strictly more with it — magic-link persists the address
// into the token store for the full TTL before any delivery check
// (magiclink.go:278), forgot-password writes it into the audit event
// even on the unknown-account branch (password_reset.go:141-146) — so
// the same bound applies. Without it, a loop of unauthenticated POSTs
// floods server-side state with attacker-sized (<=1 MiB) strings.
//
// Surfaces (one property, every surface):
//   - POST /auth/magic-link/send
//   - POST /auth/forgot-password
func TestEmailLengthCap_EmailEndpoints(t *testing.T) {
	over := strings.Repeat("a", maxEmailLen+1) // 255 chars, past the RFC 5321 bound

	// Surface: magic-link send. The mock sender records what it was
	// handed, so the pin also proves the oversized address is not carried
	// into delivery state.
	sender := &mockEmailSender{}
	mgr, _, _ := newMagicLinkManager(t, sender)
	r := mountMagicLinkRoutes(mgr)

	body, _ := json.Marshal(map[string]string{"email": over})
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [email-bound] POST /auth/magic-link/send accepted a %d-char email (limit %d): %d %s — decodeAuthCredentials rejects over-length emails at ingestion on login/register (form_decode.go:83-101); this handler instead persists the string verbatim into the token store for the full TTL before any delivery check, so each unauthenticated POST parks an attacker-sized row", len(over), maxEmailLen, w.Code, w.Body.String())
	}
	if sender.lastEmail == over {
		t.Errorf("SECURITY: [email-bound] POST /auth/magic-link/send handed the %d-char address to the email sender — oversized request input must be rejected at ingestion, not carried into delivery", len(sender.lastEmail))
	}

	// Boundary: exactly maxEmailLen is the RFC 5321 ceiling and must
	// still be processed — the demanded control is the length bound, not
	// blanket rejection.
	body, _ = json.Marshal(map[string]string{"email": strings.Repeat("b", maxEmailLen)})
	req = httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SECURITY: [email-bound] POST /auth/magic-link/send rejected an email at exactly the RFC 5321 limit (%d chars): %d %s", maxEmailLen, w.Code, w.Body.String())
	}

	// Surface: forgot-password, unknown account — the branch that still
	// writes the submitted string into the audit event.
	store := newUserStoreWithPassword()
	mgr2 := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
	})
	mgr2.Use(NewCorePlugin())
	mgr2.Use(NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL:     "http://localhost",
		TokenTTL:    time.Hour,
		EmailSender: &stubEmailSender{},
	}))
	if err := mgr2.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r2 := router.New()
	mgr2.RegisterRoutes(r2)

	body, _ = json.Marshal(map[string]string{"email": over})
	req = httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r2.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [email-bound] POST /auth/forgot-password accepted a %d-char email (limit %d): %d %s — the handler writes the submitted string into the password.reset_requested audit row even when no account matches, so repeated unauthenticated POSTs grow the audit log with megabyte-scale attacker-controlled rows", len(over), maxEmailLen, w.Code, w.Body.String())
	}
}
