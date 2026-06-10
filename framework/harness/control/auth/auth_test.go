package auth

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

func newTestEncoder(t *testing.T) *Encoder {
	t.Helper()
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	return NewEncoder(secret)
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	enc := newTestEncoder(t)
	session := ids.NewSessionID()
	claims := Claims{
		Sessions:       []ids.SessionID{session},
		Commands:       []string{"SendInput"},
		IdentityClass:  control.IdentityHuman,
		ExpiresAt:      time.Now().Add(time.Hour).Unix(),
		CriticalClaims: []string{"sessions"},
	}
	tok, err := enc.Encode(claims)
	if err != nil {
		t.Fatal(err)
	}
	got, err := enc.Decode(tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sessions[0] != session {
		t.Errorf("session mismatch: %v", got.Sessions)
	}
	if got.Ver != VerCurrent {
		t.Errorf("Ver = %d", got.Ver)
	}
	if got.JTI == "" {
		t.Error("JTI empty after round-trip")
	}
}

func TestDecodeRejectsTampered(t *testing.T) {
	enc := newTestEncoder(t)
	tok, _ := enc.Encode(ClaimsFor(ids.NewSessionID(), control.IdentityHuman))
	// Flip one byte in the payload.
	tampered := strings.Replace(tok, tok[:1], "x", 1)
	if _, err := enc.Decode(tampered); err == nil {
		t.Fatal("expected signature mismatch")
	}
}

func TestDecodeRejectsUnknownVersion(t *testing.T) {
	enc := newTestEncoder(t)
	c := ClaimsFor(ids.NewSessionID(), control.IdentityHuman)
	c.Ver = 999
	tok, _ := enc.Encode(c)
	if _, err := enc.Decode(tok); err == nil {
		t.Fatal("expected version-unsupported error")
	}
}

func TestDecodeRejectsUnknownCriticalClaim(t *testing.T) {
	enc := newTestEncoder(t)
	c := ClaimsFor(ids.NewSessionID(), control.IdentityHuman)
	c.CriticalClaims = []string{"sessions", "scopes_v2"}
	tok, _ := enc.Encode(c)
	if _, err := enc.Decode(tok); err == nil {
		t.Fatal("expected unknown critical claim error")
	}
}

func TestVerifyExpired(t *testing.T) {
	enc := newTestEncoder(t)
	c := ClaimsFor(ids.NewSessionID(), control.IdentityAgent)
	c.ExpiresAt = time.Now().Add(-time.Minute).Unix()
	tok, _ := enc.Encode(c)
	_, err := Verify(enc, nil, tok, time.Now())
	var expErr *ExpiredError
	if !errors.As(err, &expErr) {
		t.Fatalf("err = %v, want *ExpiredError", err)
	}
}

func TestRevocationListBlocksToken(t *testing.T) {
	enc := newTestEncoder(t)
	c := ClaimsFor(ids.NewSessionID(), control.IdentityHuman)
	tok, _ := enc.Encode(c)
	parsed, _ := enc.Decode(tok)
	rl := NewRevocationList()
	rl.Revoke(parsed.JTI)
	_, err := Verify(enc, rl, tok, time.Now())
	var revErr *RevokedError
	if !errors.As(err, &revErr) {
		t.Fatalf("err = %v, want *RevokedError", err)
	}
}

func TestAllowsCommandAndSession(t *testing.T) {
	session := ids.NewSessionID()
	c := Claims{Sessions: []ids.SessionID{session}, Commands: []string{"SendInput"}}
	if !c.AllowsCommand("SendInput") {
		t.Error("expected SendInput allowed")
	}
	if c.AllowsCommand("DropAllSessions") {
		t.Error("unauthorized command should be denied")
	}
	if !c.AllowsSession(session) {
		t.Error("expected session allowed")
	}
	if c.AllowsSession(ids.NewSessionID()) {
		t.Error("unauthorized session should be denied")
	}
}

func TestIssuerHappyPath(t *testing.T) {
	var ch bytes.Buffer
	enc := newTestEncoder(t)
	iss := NewIssuer(enc, PrintTTYChannel{W: &ch})

	claims := ClaimsFor(ids.NewSessionID(), control.IdentityHuman)
	mintID, err := iss.Begin(context.Background(), claims, "test session")
	if err != nil {
		t.Fatal(err)
	}
	// Extract the 6-digit code from the captured output.
	code := extractCode(t, ch.String())
	tok, err := iss.Confirm(mintID, code)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if _, err := enc.Decode(tok); err != nil {
		t.Errorf("token did not verify: %v", err)
	}
}

func TestIssuerRejectsBadCode(t *testing.T) {
	var ch bytes.Buffer
	iss := NewIssuer(newTestEncoder(t), PrintTTYChannel{W: &ch})
	mintID, err := iss.Begin(context.Background(), ClaimsFor(ids.NewSessionID(), control.IdentityHuman), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := iss.Confirm(mintID, "000000"); !errors.Is(err, ErrCodeMismatch) {
		t.Fatalf("err = %v, want %v", err, ErrCodeMismatch)
	}
}

func TestIssuerRequiresChannel(t *testing.T) {
	iss := NewIssuer(newTestEncoder(t), nil)
	_, err := iss.Begin(context.Background(), ClaimsFor(ids.NewSessionID(), control.IdentityHuman), "")
	if !errors.Is(err, ErrNoChannel) {
		t.Fatalf("err = %v, want ErrNoChannel", err)
	}
}

func extractCode(t *testing.T, s string) string {
	t.Helper()
	idx := strings.Index(s, "code: ")
	if idx < 0 {
		t.Fatalf("no code in output: %q", s)
	}
	tail := s[idx+len("code: "):]
	end := strings.IndexAny(tail, " \n")
	if end < 0 {
		end = len(tail)
	}
	return tail[:end]
}
