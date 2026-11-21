package auth

import (
	"context"
	"errors"
	"strings"
	"time"
)

type tokenPurpose string

const (
	purposeReset     tokenPurpose = "pwreset"
	purposeVerify    tokenPurpose = "verify"
	purposeMagicLink tokenPurpose = "magiclink"

	purposeEmailCode tokenPurpose = "emailcode"

	purposePasskeyRegister        tokenPurpose = "passkeyreg"
	purposePasskeyLogin           tokenPurpose = "passkeylogin"
	purposePasskeyRegisterQuiet   tokenPurpose = "passkeyregc"
	purposePasskeyReauth          tokenPurpose = "passkeyreauth"
	purposePasskeyProof           tokenPurpose = "passkeyproof"
	purposePasskeyRegisterSession tokenPurpose = "passkeyregsess"
)

var userPurposes = []tokenPurpose{purposeReset, purposeVerify, purposePasskeyRegister, purposePasskeyRegisterQuiet, purposePasskeyReauth}

var errTokenPurpose = errors.New("auth: token is not valid for this flow")

func createPurposeToken(ctx context.Context, store MagicLinkTokenStore, p tokenPurpose, payload string, ttl time.Duration) (string, error) {
	return store.CreateToken(ctx, string(p)+":"+payload, ttl)
}

func redeemPurposeToken(ctx context.Context, store MagicLinkTokenStore, p tokenPurpose, token string) (string, error) {
	_, payload, err := redeemAnyPurpose(ctx, store, token, p)
	return payload, err
}

func redeemAnyPurpose(ctx context.Context, store MagicLinkTokenStore, token string, ps ...tokenPurpose) (tokenPurpose, string, error) {
	match := func(raw string) (tokenPurpose, string, bool) {
		for _, p := range ps {
			if payload, ok := strings.CutPrefix(raw, string(p)+":"); ok {
				return p, payload, true
			}
		}
		return "", "", false
	}
	if peeker, ok := store.(MagicLinkTokenPeeker); ok {
		raw, err := peeker.PeekToken(ctx, token)
		if err != nil {
			return "", "", err
		}
		if _, _, ok := match(raw); !ok {
			return "", "", errTokenPurpose
		}
	}
	raw, err := store.RedeemToken(ctx, token)
	if err != nil {
		return "", "", err
	}
	p, payload, ok := match(raw)
	if !ok {
		return "", "", errTokenPurpose
	}
	return p, payload, nil
}

func peekPurposePayload(raw string, p tokenPurpose) (string, bool) {
	return strings.CutPrefix(raw, string(p)+":")
}

// MagicLinkTokenPurger is the MagicLinkTokenStore extension that drops every token carrying a payload.
type MagicLinkTokenPurger interface {
	// DeleteTokensForPayload deletes the unredeemed tokens whose payload equals payload and returns how many.
	DeleteTokensForPayload(ctx context.Context, payload string) (int, error)
}

func purgePurposeTokens(ctx context.Context, store MagicLinkTokenStore, p tokenPurpose, payload string) (int, bool, error) {
	purger, ok := store.(MagicLinkTokenPurger)
	if !ok {
		return 0, false, nil
	}
	n, err := purger.DeleteTokensForPayload(ctx, string(p)+":"+payload)
	return n, true, err
}
