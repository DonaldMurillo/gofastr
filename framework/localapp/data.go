package localapp

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"

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
	// DB is app.db; pass it to framework.WithDB. The caller closes it.
	DB *sql.DB
	// Secret keeps sessions valid across restarts; pass it to framework.WithSecret.
	Secret string
	// User is the installation's identity, the owner of its rows.
	User *User
}

// Close closes the database.
func (d *Data) Close() error { return d.DB.Close() }

// Open opens the absolute data dir (owner-only when it creates it) and its state.
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

// OpenDB opens dir/app.db, creating it owner-only first.
func OpenDB(dir string) (*sql.DB, error) {
	path := filepath.ToSlash(filepath.Join(dir, DBFile))
	if err := fileperm.SeedOwnerOnly(path); err != nil {
		return nil, fmt.Errorf("localapp: restrict app database: %w", err)
	}
	db, err := sql.Open("sqlite3", sqliteFileURI(path))
	if err != nil {
		return nil, fmt.Errorf("localapp: open app database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("localapp: app database unreachable: %w", err)
	}
	return db, nil
}

func sqliteFileURI(path string) string {
	return "file:" + (&url.URL{Path: path}).EscapedPath()
}

const secretBytes = 32

// LoadOrMintSecret returns dir/secret, minting a 0600 one on first run.
func LoadOrMintSecret(dir string) (string, error) {
	path := filepath.Join(dir, SecretFile)
	if b, err := readOwnerOnly(path); err != nil {
		return "", fmt.Errorf("localapp: %w", err)
	} else if b != nil {
		return checkSecret(path, b)
	}
	var raw [secretBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("localapp: mint secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(raw[:])
	if err := writeExclusive(path, []byte(secret)); err != nil {
		if errors.Is(err, os.ErrExist) {
			if b, rerr := readOwnerOnly(path); rerr == nil && b != nil {
				return checkSecret(path, b)
			}
		}
		return "", fmt.Errorf("localapp: write secret file: %w", err)
	}
	return secret, nil
}

func checkSecret(path string, b []byte) (string, error) {
	s := string(b)
	if raw, err := base64.RawURLEncoding.DecodeString(s); err != nil || len(raw) != secretBytes || base64.RawURLEncoding.EncodeToString(raw) != s {
		return "", fmt.Errorf("localapp: secret file %s is empty or damaged; delete it to mint a new one (every session signs out)", path)
	}
	return s, nil
}
