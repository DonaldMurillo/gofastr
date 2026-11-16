package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// mailboxSignInRefusal is why signInMailboxOwner minted no session: the
// phrase the caller answers with, under 401 when the credential moved
// meanwhile and 500 otherwise.
type mailboxSignInRefusal struct {
	stale  bool
	reason i18n.Phrase
}

func (r *mailboxSignInRefusal) status() int {
	if r.stale {
		return http.StatusUnauthorized
	}
	return http.StatusInternalServerError
}

// signInMailboxOwner turns proof that the caller reads email's mailbox
// into a stored session for its account. It is the one path the
// magic-link and emailed-code sign-ins share: find the account or create
// it (marked verified), claim an unverified one, mint through
// MintSession (so a 2FA account's session is pending), then re-read the
// credential so a reset that finished meanwhile outranks the sign-in.
// via names the flow in the audit trail and the claim. The caller sets
// the cookie and answers.
func (m *AuthManager) signInMailboxOwner(ctx context.Context, r *http.Request, email, via string) (User, *Session, bool, *mailboxSignInRefusal) {
	refuse := func(stale bool, reason i18n.Phrase) (User, *Session, bool, *mailboxSignInRefusal) {
		return nil, nil, false, &mailboxSignInRefusal{stale: stale, reason: reason}
	}
	userStore := m.UserStore()
	if userStore == nil {
		return refuse(false, reasonUserStoreNotConfigured)
	}

	user, hash, findErr := userStore.FindByEmail(ctx, email)
	if findErr != nil && !errors.Is(findErr, ErrUserNotFound) {
		// Any non-NotFound error is a transport / DB failure. We must
		// NOT silently auto-create a user, that would mask the failure
		// AND let an attacker provoke account creation by tripping a
		// transient lookup error.
		return refuse(false, reasonUserLookupFailed)
	}
	created, claimed := false, false
	if findErr != nil {
		// Prefer the OAuthUserCreator path so password_set=false is
		// recorded for the unlink guard; fall back to CreateUser with
		// the shared placeholder for stores that don't opt in. Either
		// way these users never authenticate with a password.
		var err error
		if creator, ok := userStore.(OAuthUserCreator); ok {
			user, err = creator.CreateUserNoPassword(ctx, email, m.DefaultRoles())
		} else {
			user, err = userStore.CreateUser(ctx, email, placeholderPasswordHash(), m.DefaultRoles())
		}
		if errors.Is(err, ErrEmailTaken) {
			// A concurrent sign-in for the same address created it
			// between the lookup and the create. This proof is spent,
			// so take the existing-account path instead of answering 500.
			user, hash, err = userStore.FindByEmail(ctx, email)
			if err != nil {
				return refuse(false, reasonUserLookupFailed)
			}
		} else if err != nil {
			return refuse(false, reasonFailedToCreateUser)
		} else {
			created = true
		}
	}
	if created {
		// The proof is the mailbox's. A failure leaves the account
		// unverified and the next sign-in claims it, a no-op on an
		// account nothing else has touched.
		if verifier, ok := userStore.(EmailVerifier); ok {
			if err := verifier.MarkEmailVerified(ctx, user.GetID()); err != nil {
				return refuse(false, reasonFailedToMarkEmailVerified)
			}
		}
	} else {
		// The address may have been registered by someone who never
		// proved it, with a password and sessions of their own. This
		// proof is the mailbox owner's, so an unverified account is
		// claimed: everything the registrant attached is evicted before
		// the owner's session is minted. A store that cannot say whether
		// the address was proven is refused rather than guessed at
		// either way: "verified" signs the owner into the squatter's
		// live account, "unverified" wipes a real owner's password on
		// every sign-in.
		checker, ok := userStore.(EmailVerifiedChecker)
		if !ok {
			return refuse(false, reasonStoreNoVerifiedChecker)
		}
		verified, err := checker.IsEmailVerified(ctx, user.GetID())
		if err != nil {
			return refuse(false, reasonUserLookupFailed)
		}
		if !verified {
			if err := m.claimAccount(ctx, user.GetID(), via, remoteHost(r), true); err != nil {
				slog.Warn("auth: account claim failed",
					"via", via, "user_hash", hashedIdentifier(user.GetID()), "err", err)
				return refuse(false, reasonAccountClaimFailed)
			}
			claimed = true
		}
	}
	if created || claimed {
		// This request wrote the credential itself; the one to hold the
		// session to is what it wrote.
		var err error
		if _, hash, err = userStore.FindByEmail(ctx, email); err != nil {
			return refuse(false, reasonUserLookupFailed)
		}
	}

	// Mint through the manager, not SessionStore().Create: MintSession is
	// what applies the second-factor pending mark, so a mailbox proof
	// alone never yields a full session for a 2FA account.
	sess, pending, err := m.MintSession(ctx, user.GetID(), m.Config().SessionTTL)
	if errors.Is(err, ErrTwoFactorEnforcement) {
		m.emitSecurity(ctx, SecurityEvent{
			Kind: "login.failed", UserID: user.GetID(), Email: email, Remote: remoteHost(r),
			Meta: map[string]string{"reason": "twofa_failclosed", "via": via},
		})
		return refuse(false, reasonTwoFactorUnavailable)
	}
	if err != nil {
		return refuse(false, reasonSessionCreateFailed)
	}
	// A password reset or change that finished after the proof was spent
	// purged this user's sessions and tokens before this session existed.
	// Re-read the credential now the session is stored, as login does: a
	// purge from here on reaches it, and a credential that already moved
	// means the reset outranks the sign-in.
	if !credentialUnchanged(ctx, userStore, email, user.GetID(), hash) {
		if err := m.SessionStore().Delete(ctx, sess.Token); err != nil {
			slog.Warn("auth: dropping a session minted under a stale credential failed",
				"via", via, "user_hash", hashedIdentifier(user.GetID()), "err", err)
		}
		return refuse(true, reasonInvalidToken)
	}
	return user, sess, pending, nil
}
