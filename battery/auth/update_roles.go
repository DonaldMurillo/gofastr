package auth

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// UpdateRoles replaces a user's roles, an empty list staying empty, or returns ErrUserNotFound.
func (s *EntityUserStore) UpdateRoles(ctx context.Context, userID string, roles []string) error {
	if !textsafe.Storable(userID) {
		return ErrUserNotFound
	}
	b, _ := json.Marshal(roles)
	rolesJSON := string(b)
	q := fmt.Sprintf(
		"UPDATE %s SET %s = $1 WHERE %s = $2",
		query.QuoteFolded(s.table),
		query.QuoteFolded(s.fieldMap.Roles),
		query.QuoteFolded(s.fieldMap.ID),
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
