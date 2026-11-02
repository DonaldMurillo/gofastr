package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
// user's password: the hash becomes a new unusable hash, different from
// the one stored before so the JWTs bound to it end, and the store
// records that no password is set. A magic-link claim uses it to evict a password
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
//  1. every session is deleted (SessionUserPurger), so a request that
//     attaches a credential under one (an OAuth link callback, a token or
//     passkey registration) finds its session gone when it re-checks after
//     writing, even when its write lands after the steps below read the
//     account;
//  2. every OAuth link is removed (AccountLister + AccountUnlinker), and
//     the provider tokens stored for the account are deleted;
//  3. with clearPassword, the password is removed (PasswordClearer, or
//     PasswordSetter with the placeholder hash);
//  4. every passkey is removed and every API token issued through
//     TokensPlugin is revoked;
//  5. every session is deleted again: a sign-in through a credential
//     steps 2-4 removed may have minted one since step 1. Every step runs
//     even when an earlier one fails, so one failing store does not leave
//     the others' credentials live, and any failure ends the claim here
//     with the joined error;
//  6. the account is marked verified (EmailVerifier), last, so a failure
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

	// A missing extension is configuration, refused before any step runs.
	lister, unlinker, err := oauthUnlinkTools(store)
	if err != nil {
		return err
	}
	clearer, _ := store.(PasswordClearer)
	setter, _ := store.(PasswordSetter)
	if clearPassword && clearer == nil && setter == nil {
		return errors.New("user store implements neither PasswordClearer nor PasswordSetter")
	}

	// Every step runs even when an earlier one fails: a failing session
	// store must not leave the OAuth links, password, passkeys or tokens
	// live, nor a failed passkey purge the tokens. Any failure still stops
	// the claim before step 6.
	var failed []error
	early, err := purger.DeleteByUser(ctx, userID)
	if err != nil {
		failed = append(failed, fmt.Errorf("revoke sessions: %w", err))
	}

	removed, err := m.unlinkAllOAuth(ctx, userID, lister, unlinker)
	if err != nil {
		failed = append(failed, err)
	}
	unlinked := len(removed)
	// unlinkAllOAuth drops the tokens of the links it removed; a retry
	// after a failed claim has no links left to list, so the registered
	// providers' rows go too. A failure here does not stop the purges
	// below: the claim fails after them, before the mark, so the next
	// proof retries it.
	tokenErr := m.dropProviderTokens(ctx, userID, removed)

	if clearPassword {
		if err := clearOrPlaceholder(ctx, clearer, setter, userID); err != nil {
			failed = append(failed, fmt.Errorf("clear password: %w", err))
		}
	}

	passkeys, err := m.purgeUserPasskeys(ctx, userID)
	if err != nil {
		failed = append(failed, fmt.Errorf("remove passkeys: %w", err))
	}
	tokens, _, err := m.revokeUserAPITokens(ctx, userID)
	if err != nil {
		failed = append(failed, fmt.Errorf("revoke API tokens: %w", err))
	}
	late, err := purger.DeleteByUser(ctx, userID)
	if err != nil {
		failed = append(failed, fmt.Errorf("revoke sessions: %w", err))
	}
	if tokenErr != nil {
		failed = append(failed, fmt.Errorf("drop provider tokens: %w", tokenErr))
	}
	if len(failed) > 0 {
		return errors.Join(failed...)
	}
	sessions := early + late
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
			"passkeys_removed":    strconv.Itoa(passkeys),
		},
	})
	return nil
}

// oauthUnlinkTools returns the store's lister and unlinker when it links
// OAuth identities, and nils when it links none. A store that links but
// cannot list or unlink is a configuration error: a reset or a claim that
// cannot remove a planted IdP identity must not report success.
func oauthUnlinkTools(store UserStore) (AccountLister, AccountUnlinker, error) {
	if _, isLinker := store.(OAuthLinker); !isLinker {
		return nil, nil, nil
	}
	lister, lok := store.(AccountLister)
	unlinker, uok := store.(AccountUnlinker)
	if !lok || !uok {
		return nil, nil, errors.New("user store links OAuth identities but cannot list and unlink them")
	}
	return lister, unlinker, nil
}

// unlinkAllOAuth removes every OAuth identity linked to userID and the
// provider tokens stored for it, and returns the providers it unlinked. A
// failed unlink does not stop the rest; the failures come back joined.
// lister nil means the store links no identities.
func (m *AuthManager) unlinkAllOAuth(ctx context.Context, userID string, lister AccountLister, unlinker AccountUnlinker) ([]string, error) {
	if lister == nil {
		return nil, nil
	}
	accts, err := lister.ListAccounts(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list OAuth links: %w", err)
	}
	var tokens OAuthTokenStore
	if op, err := PluginAs[*OAuth2Plugin](m, "oauth2"); err == nil {
		tokens = op.TokenStore()
	}
	var removed []string
	var failed []error
	for _, a := range accts {
		if err := unlinker.UnlinkOAuth(ctx, userID, a.Provider); err != nil {
			failed = append(failed, fmt.Errorf("unlink %s: %w", a.Provider, err))
			continue
		}
		removed = append(removed, a.Provider)
		// The stored refresh token outlives the link otherwise, and is
		// password-equivalent at the provider.
		if tokens != nil {
			if err := tokens.Delete(ctx, userID, a.Provider); err != nil {
				failed = append(failed, fmt.Errorf("delete %s provider tokens: %w", a.Provider, err))
			}
		}
	}
	return removed, errors.Join(failed...)
}

// clearOrPlaceholder removes userID's password through clearer when the
// store has one, else overwrites it with a fresh unusable hash through
// setter. One of the two is non-nil (claimAccount checks before it runs).
func clearOrPlaceholder(ctx context.Context, clearer PasswordClearer, setter PasswordSetter, userID string) error {
	if clearer != nil {
		return clearer.ClearPassword(ctx, userID)
	}
	placeholder, err := newPlaceholderHash()
	if err != nil {
		return err
	}
	return setter.SetPassword(ctx, userID, placeholder)
}

// dropProviderTokens deletes the OAuthTokenStore rows a claimed account
// holds: one per provider the claim unlinked, and one per provider the
// OAuth2 plugin registers. The registered set is what makes a retry
// complete: once the links are gone the claim cannot list them again,
// and a row left by a failed earlier attempt still has to go. Every
// delete runs; the failures come back joined. A wiring with no OAuth2
// plugin, or no TokenStore, never stored provider tokens.
func (m *AuthManager) dropProviderTokens(ctx context.Context, userID string, unlinked []string) error {
	op, err := PluginAs[*OAuth2Plugin](m, "oauth2")
	if err != nil {
		return nil
	}
	ts := op.TokenStore()
	if ts == nil {
		return nil
	}
	keys := append(op.providerKeys(), unlinked...)
	slices.Sort(keys)
	var errs []error
	for _, key := range slices.Compact(keys) {
		if err := ts.Delete(ctx, userID, key); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		}
	}
	return errors.Join(errs...)
}
