package auth

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// UpdateRoles replaces the roles for an existing user. It writes the JSON
// roles column via a parameterized UPDATE with query.QuoteIdent on the
// column name. Returns ErrUserNotFound when no row matches.
//
// Unlike CreateUser (which uses formatRoles and defaults empty roles to
// ["user"]), UpdateRoles serializes the slice directly via json.Marshal so
// an empty roles list clears the column (writes "null") rather than
// silently stamping the default. Clearing roles is a legitimate admin
// operation; defaulting would be a privilege bug.
//
// The roles are OPERATOR input (from an admin screen), never request data —
// the same security posture as AuthConfig.DefaultRoles. The caller
// (AuthManager.SetUserRoles) is responsible for sourcing them from an
// admin-gated context.
func (s *EntityUserStore) UpdateRoles(ctx context.Context, userID string, roles []string) error {
	// json.Marshal directly — do NOT use formatRoles, which defaults empty
	// to ["user"]. An admin clearing a user's roles must produce an empty
	// set, not a silent promotion to "user".
	b, _ := json.Marshal(roles)
	rolesJSON := string(b)
	q := fmt.Sprintf(
		"UPDATE %s SET %s = $1 WHERE %s = $2",
		query.QuoteIdent(s.table),
		query.QuoteIdent(s.fieldMap.Roles),
		query.QuoteIdent(s.fieldMap.ID),
	)
	res, err := s.db.ExecContext(ctx, q, rolesJSON, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}
