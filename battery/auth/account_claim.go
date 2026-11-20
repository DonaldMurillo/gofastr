package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
)

// EmailVerifiedChecker reports whether a user proved their address.
type EmailVerifiedChecker interface {
	IsEmailVerified(ctx context.Context, userID string) (bool, error)
}

// PasswordClearer replaces a user's password with a fresh unusable hash and records none set.
type PasswordClearer interface {
	ClearPassword(ctx context.Context, userID string) error
}

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

	lister, unlinker, err := oauthUnlinkTools(store)
	if err != nil {
		return err
	}
	clearer, _ := store.(PasswordClearer)
	setter, _ := store.(PasswordSetter)
	if clearPassword && clearer == nil && setter == nil {
		return errors.New("user store implements neither PasswordClearer nor PasswordSetter")
	}

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
		if tokens != nil {
			if err := tokens.Delete(ctx, userID, a.Provider); err != nil {
				failed = append(failed, fmt.Errorf("delete %s provider tokens: %w", a.Provider, err))
			}
		}
	}
	return removed, errors.Join(failed...)
}

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
