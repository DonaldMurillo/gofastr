package auth

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

func TestNewEncoderRefusesShortSecret(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewEncoder accepted a 31-byte secret")
		}
	}()
	NewEncoder(make([]byte, 31))
}

func TestEmptyScopeAllowsAll(t *testing.T) {
	var c Claims
	if !c.AllowsCommand("SendInput") || !c.AllowsSession(ids.NewSessionID()) {
		t.Fatal("empty Commands/Sessions must allow any command and session")
	}
}

func TestVerifyRefusesNotYetValid(t *testing.T) {
	enc := newTestEncoder(t)
	c := ClaimsFor(ids.NewSessionID(), control.IdentityHuman)
	c.NotBefore = time.Now().Add(time.Hour).Unix()
	tok, _ := enc.Encode(c)
	if _, err := Verify(enc, nil, tok, time.Now()); err == nil {
		t.Fatal("Verify accepted a token before its nbf")
	}
}

func TestWrongCodeBurnsMint(t *testing.T) {
	var ch bytes.Buffer
	iss := NewIssuer(newTestEncoder(t), PrintTTYChannel{W: &ch})
	mintID, err := iss.Begin(context.Background(), ClaimsFor(ids.NewSessionID(), control.IdentityHuman), "")
	if err != nil {
		t.Fatal(err)
	}
	code := extractCode(t, ch.String())
	wrong := "000000"
	if code == wrong {
		wrong = "000001"
	}
	_, _ = iss.Confirm(mintID, wrong)
	if _, err := iss.Confirm(mintID, code); !errors.Is(err, ErrUnknownMint) {
		t.Fatalf("right code after a wrong one: err = %v, want ErrUnknownMint", err)
	}
}

func TestConfirmRefusesExpiredCode(t *testing.T) {
	var ch bytes.Buffer
	now := time.Now()
	iss := NewIssuer(newTestEncoder(t), PrintTTYChannel{W: &ch}).WithClock(func() time.Time { return now })
	mintID, err := iss.Begin(context.Background(), ClaimsFor(ids.NewSessionID(), control.IdentityHuman), "")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(61 * time.Second)
	if _, err := iss.Confirm(mintID, extractCode(t, ch.String())); !errors.Is(err, ErrCodeExpired) {
		t.Fatalf("err = %v, want ErrCodeExpired", err)
	}
}

func TestDecodeRejectsOtherSecret(t *testing.T) {
	tok, _ := newTestEncoder(t).Encode(ClaimsFor(ids.NewSessionID(), control.IdentityHuman))
	if _, err := newTestEncoder(t).Decode(tok); err == nil {
		t.Fatal("a token signed under another secret decoded")
	}
}
