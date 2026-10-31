package auth

import (
	"context"
	"testing"
)

// CompareAndSwapTwoFA checked the secret in Go after its read, but the
// UPDATE predicated on version, codes and flags only. A disable plus
// re-enrol between the read and the UPDATE inserted pending B at
// version 0 with the same empty codes and false flags, so verify's swap
// for pending A matched B and marked it verified with A's secret.
func TestTwoFACASRefusesReenrolledRow(t *testing.T) {
	s := newTwoFAStore(t)
	ctx := context.Background()
	pendingA := &TwoFAState{Secret: "AAAAAAAAAAAAAAAA"}
	if err := s.SetTwoFA(ctx, "u1", pendingA); err != nil {
		t.Fatal(err)
	}
	expect, err := s.GetTwoFA(ctx, "u1")
	if err != nil || expect == nil {
		t.Fatalf("GetTwoFA: %v %v", expect, err)
	}

	fired := false
	s.casTestHook = func() {
		if fired {
			return
		}
		fired = true
		if err := s.DeleteTwoFA(ctx, "u1"); err != nil {
			t.Fatalf("hook delete: %v", err)
		}
		if err := s.SetTwoFA(ctx, "u1", &TwoFAState{Secret: "BBBBBBBBBBBBBBBB"}); err != nil {
			t.Fatalf("hook re-enrol: %v", err)
		}
	}
	verified := &TwoFAState{Enabled: true, Verified: true, Secret: "AAAAAAAAAAAAAAAA"}
	ok, err := s.CompareAndSwapTwoFA(ctx, "u1", expect, verified)
	if err != nil {
		t.Fatal(err)
	}
	if !fired {
		t.Fatal("casTestHook never fired: the swap has no hook between its read and its write")
	}
	if ok {
		t.Fatal("the swap for pending A landed on re-enrolled pending B")
	}
	got, err := s.GetTwoFA(ctx, "u1")
	if err != nil || got == nil {
		t.Fatalf("GetTwoFA after: %v %v", got, err)
	}
	if got.Verified || got.Secret != "BBBBBBBBBBBBBBBB" {
		t.Fatalf("row after the refused swap = verified %v secret %q, want pending B", got.Verified, got.Secret)
	}
}
