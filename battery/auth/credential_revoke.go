package auth

import (
	"context"
	"errors"
)

// revokeUserAPITokens revokes every live API token userID owns, through the
// store the TokensPlugin was built with. A credential reset that left these
// alive did not lock out whoever held the account before it: a token minted
// with the old password kept authenticating afterwards.
//
// supported is false when no TokensPlugin is registered; tokens a host mints
// with IssueToken against its own store are the host's to revoke.
func (m *AuthManager) revokeUserAPITokens(ctx context.Context, userID string) (n int, supported bool, err error) {
	p, ok := m.Plugin("api-tokens")
	if !ok {
		return 0, false, nil
	}
	tp, ok := p.(*TokensPlugin)
	if !ok || tp.tokens == nil {
		return 0, false, nil
	}
	list, err := tp.tokens.List(ctx, "user", userID)
	if err != nil {
		return 0, true, err
	}
	for _, t := range list {
		if t.RevokedAt != nil {
			continue
		}
		if err := tp.tokens.Revoke(ctx, t.ID, "user", userID); err != nil {
			if errors.Is(err, ErrTokenNotFound) {
				continue
			}
			return n, true, err
		}
		n++
	}
	return n, true, nil
}
