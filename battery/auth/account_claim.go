package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

// EmailVerifiedChecker is the optional UserStore extension that reports
// whether a user proved control of their email address. MagicLinkPlugin
// requires it for existing accounts, OAuth2Plugin auto-links only into
// accounts it reports verified, and PasswordResetPlugin claims accounts it
// reports unverified. EntityUserStore implements it over its
// email_verified column.
type EmailVerifiedChecker interface {
	IsEmailVerified(ctx context.Context, userID string) (bool, error)
}

// PasswordClearer is the optional UserStore extension that removes a
// user's password: the hash becomes the placeholder and the store records
// that no password is set. A magic-link claim uses it to evict a password
// chosen by whoever registered the address before its owner.
type PasswordClearer interface {
	ClearPassword(ctx context.Context, userID string) error
}

// claimAccount hands an unverified account to the person who just proved
// control of its email address. Anyone can register an address, or sign in
// through an IdP that does not verify it, before the mailbox owner shows
// up. Whatever that person attached to the account is evicted here, in an
// order that closes their routes back in before the sessions go:
//
//  1. every OAuth link is removed (AccountLister + AccountUnlinker);
//  2. with clearPassword, the password is removed (PasswordClearer, or
//     PasswordSetter with the placeholder hash);
//  3. every API token issued through TokensPlugin is revoked;
//  4. every session is deleted (SessionUserPurger);
//  5. the account is marked verified (EmailVerifier), last, so a failure
//     at any earlier step leaves it unverified and the next proof retries
//     the whole claim.
//
// A missing extension the claim needs is an error, never a skipped step.
// An enrolled second factor stays: proving the mailbox is one factor, and
// every account that predates email_verified reads unverified, so
// stripping 2FA here would let a mailbox alone remove it from them all.
func (m *AuthManager) claimAccount(ctx context.Context, userID, via, remote string, clearPassword bool) error {
	store := m.UserStore()
	verifier, ok := store.(EmailVerifier)
	if !ok {
		return errors.New("user store does not implement EmailVerifier")
	}
	purger, ok := m.SessionStore().(SessionUserPurger)
	if !ok {
		return errors.New("session store does not implement SessionUserPurger")
	}

	unlinked := 0
	if _, isLinker := store.(OAuthLinker); isLinker {
		lister, lok := store.(AccountLister)
		unlinker, uok := store.(AccountUnlinker)
		if !lok || !uok {
			return errors.New("user store links OAuth identities but cannot list and unlink them")
		}
		accts, err := lister.ListAccounts(ctx, userID)
		if err != nil {
			return fmt.Errorf("list OAuth links: %w", err)
		}
		for _, a := range accts {
			if err := unlinker.UnlinkOAuth(ctx, userID, a.Provider); err != nil {
				return fmt.Errorf("unlink %s: %w", a.Provider, err)
			}
			unlinked++
		}
	}

	if clearPassword {
		if clearer, ok := store.(PasswordClearer); ok {
			if err := clearer.ClearPassword(ctx, userID); err != nil {
				return fmt.Errorf("clear password: %w", err)
			}
		} else if setter, ok := store.(PasswordSetter); ok {
			if err := setter.SetPassword(ctx, userID, passwordPlaceholderHash); err != nil {
				return fmt.Errorf("clear password: %w", err)
			}
		} else {
			return errors.New("user store implements neither PasswordClearer nor PasswordSetter")
		}
	}

	tokens, _, err := m.revokeUserAPITokens(ctx, userID)
	if err != nil {
		return fmt.Errorf("revoke API tokens: %w", err)
	}
	sessions, err := purger.DeleteByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	if err := verifier.MarkEmailVerified(ctx, userID); err != nil {
		return fmt.Errorf("mark verified: %w", err)
	}

	m.emitSecurity(ctx, SecurityEvent{
		Kind:   "account.claimed",
		UserID: userID,
		Remote: remote,
		Meta: map[string]string{
			"via":                 via,
			"password_cleared":    strconv.FormatBool(clearPassword),
			"sessions_revoked":    strconv.Itoa(sessions),
			"tokens_revoked":      strconv.Itoa(tokens),
			"oauth_links_removed": strconv.Itoa(unlinked),
		},
	})
	return nil
}
