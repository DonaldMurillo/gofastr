package localapp

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"

	// OpenDB opens app.db under the "sqlite3" driver name; the blank
	// import registers it so a host needs no import of its own.
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// The files Open keeps in the data directory.
const (
	DBFile     = "app.db"
	SecretFile = "secret"
)

// Data is what a local app's data directory holds, opened.
type Data struct {
	// Dir is the absolute data directory.
	Dir string
	// DB is app.db, opened through sqlite/stdlib. Pass it to
	// framework.WithDB. The caller closes it, or calls Close.
	DB *sql.DB
	// Secret keeps sessions valid across restarts. Pass it to
	// framework.WithSecret.
	Secret string
	// User is the installation's identity, the owner of every row the
	// app writes for its one person.
	User *User
}

// Close closes the database.
func (d *Data) Close() error { return d.DB.Close() }

// Open opens the data directory dir, creating it owner-only (0700) when
// it is missing, and returns its database, secret and identity, minting
// the secret and the identity on first run. dir must be absolute: it is
// the directory the platform gives the app (Application Support, an
// Android app's files dir), never one resolved from the working
// directory. An existing directory keeps its mode: Open never tightens a
// directory it did not create.
func Open(dir string) (*Data, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("localapp: data dir %q must be an absolute path", dir)
	}
	if err := fileperm.MkdirPrivate(dir); err != nil {
		return nil, fmt.Errorf("localapp: create data dir %s: %w", dir, err)
	}
	db, err := OpenDB(dir)
	if err != nil {
		return nil, err
	}
	secret, err := LoadOrMintSecret(dir)
	if err != nil {
		db.Close()
		return nil, err
	}
	user, err := LoadOrMint(dir)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("localapp: %w", err)
	}
	return &Data{Dir: dir, DB: db, Secret: secret, User: user}, nil
}

// OpenDB opens (creating if needed) the SQLite database dir/app.db. The
// file is created owner-only before SQLite opens it, so it never exists
// with the process umask's mode.
func OpenDB(dir string) (*sql.DB, error) {
	path := filepath.ToSlash(filepath.Join(dir, DBFile))
	if err := fileperm.SeedOwnerOnly(path); err != nil {
		return nil, fmt.Errorf("localapp: restrict app database: %w", err)
	}
	db, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		return nil, fmt.Errorf("localapp: open app database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("localapp: app database unreachable: %w", err)
	}
	return db, nil
}

// LoadOrMintSecret returns the secret stored in dir/secret, minting 32
// bytes of crypto/rand (base64url) and persisting them 0600 on first
// run. Two processes racing the first boot converge on one secret: the
// file is created exclusively, and the loser reads the winner's.
func LoadOrMintSecret(dir string) (string, error) {
	path := filepath.Join(dir, SecretFile)
	if b, err := readIfExists(path); err != nil {
		return "", fmt.Errorf("localapp: %w", err)
	} else if b != nil {
		return checkSecret(path, b)
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("localapp: mint secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(raw[:])
	if err := writeExclusive(path, []byte(secret)); err != nil {
		if errors.Is(err, os.ErrExist) {
			if b, rerr := readIfExists(path); rerr == nil && b != nil {
				return checkSecret(path, b)
			}
		}
		return "", fmt.Errorf("localapp: write secret file: %w", err)
	}
	return secret, nil
}

// checkSecret refuses an empty or whitespace-damaged secret file rather
// than signing sessions with it.
func checkSecret(path string, b []byte) (string, error) {
	s := string(b)
	if s == "" || strings.TrimSpace(s) != s {
		return "", fmt.Errorf("localapp: secret file %s is empty or damaged; delete it to mint a new one (every session signs out)", path)
	}
	return s, nil
}
