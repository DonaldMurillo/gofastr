package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// RoleSwapper is the UserStore extension required by the admin role editor.
type RoleSwapper interface {
	// SwapRoles writes only if the user's role set still equals previous.
	// A missing user or a mismatch returns false, nil.
	SwapRoles(ctx context.Context, userID string, previous, roles []string) (bool, error)
}

// UpdateRoles replaces a user's roles, an empty list staying empty, or returns ErrUserNotFound.
func (s *EntityUserStore) UpdateRoles(ctx context.Context, userID string, roles []string) error {
	if !textsafe.Storable(userID) {
		return ErrUserNotFound
	}
	updated, err := s.writeRoles(ctx, userID, roles, nil)
	if err == nil && !updated {
		return ErrUserNotFound
	}
	return err
}

// SwapRoles atomically replaces an existing user's roles if they still
// match previous, allowing for legacy encodings, order and duplicates.
func (s *EntityUserStore) SwapRoles(ctx context.Context, userID string, previous, roles []string) (bool, error) {
	if !textsafe.Storable(userID) {
		return false, ErrUserNotFound
	}
	q := fmt.Sprintf("SELECT CAST(%s AS TEXT) FROM %s WHERE %s = $1",
		query.QuoteFolded(s.fieldMap.Roles), query.QuoteFolded(s.table), query.QuoteFolded(s.fieldMap.ID))
	if live := s.liveRows(); live != "" {
		q += " AND " + live
	}
	var stored string
	err := s.db.QueryRowContext(ctx, q, userID).Scan(&stored)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !maps.Equal(roleSet(parseRoles(stored)), roleSet(previous)) {
		return false, nil
	}
	return s.writeRoles(ctx, userID, roles, &stored)
}

func roleSet(roles []string) map[string]struct{} {
	set := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		set[role] = struct{}{}
	}
	return set
}

func (s *EntityUserStore) writeRoles(ctx context.Context, userID string, roles []string, previous *string) (bool, error) {
	b, _ := json.Marshal(roles)
	roleColumn := query.QuoteFolded(s.fieldMap.Roles)
	q := fmt.Sprintf(
		"UPDATE %s SET %s = $1 WHERE %s = $2",
		query.QuoteFolded(s.table),
		roleColumn,
		query.QuoteFolded(s.fieldMap.ID),
	)
	args := []any{string(b), userID}
	if previous != nil {
		collation := `"BINARY"`
		if s.sqlDialect() == migrate.DialectPostgres {
			collation = `"C"`
		}
		q += fmt.Sprintf(" AND CAST(%s AS TEXT) COLLATE %s = $3", roleColumn, collation)
		if live := s.liveRows(); live != "" {
			q += " AND " + live
		}
		args = append(args, *previous)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
