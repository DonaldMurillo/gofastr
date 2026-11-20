package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

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
		return refuse(false, reasonUserLookupFailed)
	}
	created, claimed := false, false
	if findErr != nil {
		var err error
		if creator, ok := userStore.(OAuthUserCreator); ok {
			user, err = creator.CreateUserNoPassword(ctx, email, m.DefaultRoles())
		} else {
			user, err = userStore.CreateUser(ctx, email, placeholderPasswordHash(), m.DefaultRoles())
		}
		if errors.Is(err, ErrEmailTaken) {
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
		if verifier, ok := userStore.(EmailVerifier); ok {
			if err := verifier.MarkEmailVerified(ctx, user.GetID()); err != nil {
				return refuse(false, reasonFailedToMarkEmailVerified)
			}
		}
	} else {
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
		var err error
		if _, hash, err = userStore.FindByEmail(ctx, email); err != nil {
			return refuse(false, reasonUserLookupFailed)
		}
	}

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
	if !credentialUnchanged(ctx, userStore, email, user.GetID(), hash) {
		if err := m.SessionStore().Delete(ctx, sess.Token); err != nil {
			slog.Warn("auth: dropping a session minted under a stale credential failed",
				"via", via, "user_hash", hashedIdentifier(user.GetID()), "err", err)
		}
		return refuse(true, reasonInvalidToken)
	}
	return user, sess, pending, nil
}
