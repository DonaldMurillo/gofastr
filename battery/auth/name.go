package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// NameStore is the optional UserStore extension that keeps a user's
// display name: what an app shell shows for the signed-in user (an
// account menu, an avatar's initials) in place of their email. A name
// is optional; "" means none.
type NameStore interface {
	// UserName returns the user's display name, "" when none is set,
	// and ErrUserNotFound for an unknown user.
	UserName(ctx context.Context, userID string) (string, error)
	// SetUserName stores name after CleanName, so "" clears it. It
	// returns ErrInvalidName for a name CleanName refuses and
	// ErrUserNotFound for an unknown user.
	SetUserName(ctx context.Context, userID, name string) error
}

// MaxNameRunes caps a display name's length.
const MaxNameRunes = 100

// ErrInvalidName is a display name CleanName refuses.
var ErrInvalidName = errors.New("auth: invalid display name")

// CleanName trims a display name and checks it: valid UTF-8, at most
// MaxNameRunes runes, and none of the control, bidi or zero-width
// characters that forge or reorder the text around it (a name is drawn
// in other people's screens). It returns ErrInvalidName otherwise.
func CleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > MaxNameRunes || textsafe.ContainsUnsafe(name) {
		return "", ErrInvalidName
	}
	return name, nil
}

// ensureNameColumn adds the display-name column to a table made before
// it existed, as '' for every row.
func (s *EntityUserStore) ensureNameColumn(ctx context.Context) error {
	ddl := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s TEXT NOT NULL DEFAULT ''",
		query.QuoteIdent(s.table), query.QuoteIdent(s.fieldMap.Name))
	if migrate.DetectDialect(s.db) == migrate.DialectPostgres {
		ddl = fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s TEXT NOT NULL DEFAULT ''",
			query.QuoteIdent(s.table), query.QuoteIdent(s.fieldMap.Name))
	} else {
		has, err := sqliteTableHasColumn(ctx, s.db, s.table, s.fieldMap.Name)
		if err != nil || has {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, ddl)
	return err
}

// UserName reads the user's display name. Implements NameStore.
func (s *EntityUserStore) UserName(ctx context.Context, userID string) (string, error) {
	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1",
		query.QuoteIdent(s.fieldMap.Name),
		query.QuoteIdent(s.table),
		query.QuoteIdent(s.fieldMap.ID),
	)
	var name string
	err := s.db.QueryRowContext(ctx, q, userID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUserNotFound
	}
	return name, err
}

// SetUserName stores the user's display name after CleanName.
// Implements NameStore.
func (s *EntityUserStore) SetUserName(ctx context.Context, userID, name string) error {
	name, err := CleanName(name)
	if err != nil {
		return err
	}
	q := fmt.Sprintf("UPDATE %s SET %s = $1 WHERE %s = $2",
		query.QuoteIdent(s.table),
		query.QuoteIdent(s.fieldMap.Name),
		query.QuoteIdent(s.fieldMap.ID),
	)
	res, err := s.db.ExecContext(ctx, q, name, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	return nil
}
