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

// NameStore is the UserStore extension that keeps a user's display name.
type NameStore interface {
	// UserName returns the display name, "" for none, or ErrUserNotFound.
	UserName(ctx context.Context, userID string) (string, error)
	// SetUserName stores a CleanName-checked name; it returns ErrInvalidName or ErrUserNotFound.
	SetUserName(ctx context.Context, userID, name string) error
}

// MaxNameRunes caps a display name's length.
const MaxNameRunes = 100

// ErrInvalidName is a display name CleanName refuses.
var ErrInvalidName = errors.New("auth: invalid display name")

// CleanName trims a display name and returns ErrInvalidName for bad UTF-8, over MaxNameRunes, or unsafe characters.
func CleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > MaxNameRunes || textsafe.ContainsUnsafe(name) {
		return "", ErrInvalidName
	}
	return name, nil
}

func (s *EntityUserStore) ensureNameColumn(ctx context.Context) error {
	ddl := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s TEXT NOT NULL DEFAULT ''",
		query.QuoteFolded(s.table), query.QuoteFolded(s.fieldMap.Name))
	if s.dialect.of(s.db) == migrate.DialectPostgres {
		ddl = fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s TEXT NOT NULL DEFAULT ''",
			query.QuoteFolded(s.table), query.QuoteFolded(s.fieldMap.Name))
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
		query.QuoteFolded(s.fieldMap.Name),
		query.QuoteFolded(s.table),
		query.QuoteFolded(s.fieldMap.ID),
	)
	var name string
	err := s.db.QueryRowContext(ctx, q, userID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUserNotFound
	}
	return name, err
}

// SetUserName stores the user's display name after CleanName.
func (s *EntityUserStore) SetUserName(ctx context.Context, userID, name string) error {
	name, err := CleanName(name)
	if err != nil {
		return err
	}
	q := fmt.Sprintf("UPDATE %s SET %s = $1 WHERE %s = $2",
		query.QuoteFolded(s.table),
		query.QuoteFolded(s.fieldMap.Name),
		query.QuoteFolded(s.fieldMap.ID),
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
