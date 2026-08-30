package auth

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Password reset, email verification and magic-link login are documented to
// share one MagicLinkTokenStore, and its payload is a single opaque string:
// CreateToken(ctx, payload, ttl) / RedeemToken(ctx, token) (payload, error).
// Nothing in that shape says which flow minted a token, so every handler
// redeemed any token the store held and then read the payload as whatever
// its own flow expects.
//
// The sharp edge was verification-token-resets-password. A verification
// token is minted behind only the user's own session, carries a 24h TTL,
// and is delivered as a low-sensitivity GET link — and it redeemed at
// /auth/reset-password, where its userID payload reached SetPassword. A
// reset token went the other way: redeemed at /auth/magic-link/verify its
// userID was looked up as an email address, missed, and auto-created an
// account keyed by the victim's user id.
//
// The payload is tagged with its purpose at creation and required to match
// at redemption, so the store stays one namespace while a token is only
// spendable in the flow that minted it. Tagging lives here rather than in
// the store interface because the store is the host's: a host wiring its
// own MagicLinkTokenStore gets the separation without implementing
// anything new.
//
// Untagged payloads are refused. Tokens outlive a deploy by at most their
// TTL (1h reset, 24h verification), and a token minted before this change
// carries no evidence of which flow it belongs to — accepting it would
// keep the confusion open for exactly as long as an attacker needs.
type tokenPurpose string

const (
	purposeReset     tokenPurpose = "pwreset"
	purposeVerify    tokenPurpose = "verify"
	purposeMagicLink tokenPurpose = "magiclink"
)

// errTokenPurpose is returned when a token is valid but belongs to another
// flow. Callers answer it exactly as they answer an unknown token: telling
// the holder which flow a token DOES belong to is free reconnaissance.
var errTokenPurpose = errors.New("auth: token is not valid for this flow")

// createPurposeToken mints a token whose payload is bound to one flow.
func createPurposeToken(ctx context.Context, store MagicLinkTokenStore, p tokenPurpose, payload string, ttl time.Duration) (string, error) {
	return store.CreateToken(ctx, string(p)+":"+payload, ttl)
}

// redeemPurposeToken consumes a token and returns its payload only when the
// token was minted for p.
//
// A token belonging to another flow must survive the attempt. RedeemToken is
// atomic and single-use, so checking the purpose after calling it would let
// anyone holding a victim's magic link destroy it by replaying it at
// /auth/reset-password: refused there, and gone from its own flow. Where the
// store can peek, the purpose is checked before anything is consumed.
//
// A store without MagicLinkTokenPeeker cannot offer that, and the flow
// separation is the more important of the two guarantees, so the check still
// happens — after the fact, at the cost of burning a misdirected token.
// MemoryMagicLinkTokenStore and SQLMagicLinkTokenStore both peek.
func redeemPurposeToken(ctx context.Context, store MagicLinkTokenStore, p tokenPurpose, token string) (string, error) {
	if peeker, ok := store.(MagicLinkTokenPeeker); ok {
		raw, err := peeker.PeekToken(ctx, token)
		if err != nil {
			return "", err
		}
		if _, ok := peekPurposePayload(raw, p); !ok {
			return "", errTokenPurpose
		}
	}
	raw, err := store.RedeemToken(ctx, token)
	if err != nil {
		return "", err
	}
	payload, ok := strings.CutPrefix(raw, string(p)+":")
	if !ok {
		return "", errTokenPurpose
	}
	return payload, nil
}

// peekPurposePayload reads a token's payload without consuming it, for the
// magic-link confirmation page. A token from another flow reads as unknown.
func peekPurposePayload(raw string, p tokenPurpose) (string, bool) {
	return strings.CutPrefix(raw, string(p)+":")
}
