package mobilehost

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib" // registers "sqlite3" (modernc, pure Go)
)

// PROTOTYPE FINDING: everything in this file is a copy. battery/desktop
// does the same work in datadir.go with internal/fileperm and
// internal/atomicfile, which a module outside github.com/DonaldMurillo/
// gofastr cannot import. A real split needs those helpers public (or a
// public "local data dir" package shared by desktop and mobile), or the
// security-sensitive parts (owner-only modes, exclusive create) drift.

// Options opens the app's data directory and returns the framework
// options a local-first app runs on: its SQLite database and its session
// secret. dataDir is the app sandbox directory the native side passes in
// (iOS: Application Support inside the app container; Android:
// Context.getFilesDir()). os.UserConfigDir is not used: on Android it
// depends on $HOME, which an app process does not have.
func Options(dataDir string) ([]framework.AppOption, error) {
	if err := ensurePrivateDir(dataDir); err != nil {
		return nil, err
	}
	db, err := openDB(dataDir)
	if err != nil {
		return nil, err
	}
	secret, err := loadOrMint(filepath.Join(dataDir, "secret"), nil)
	if err != nil {
		db.Close()
		return nil, err
	}
	return []framework.AppOption{framework.WithDB(db), framework.WithSecret(secret)}, nil
}

func ensurePrivateDir(dir string) error {
	if dir == "" || !filepath.IsAbs(dir) {
		return fmt.Errorf("mobilehost: data dir %q must be an absolute path", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mobilehost: create data dir: %w", err)
	}
	return nil
}

func openDB(dir string) (*sql.DB, error) {
	path := filepath.Join(dir, "app.db")
	// Create the file owner-only before SQLite does, so it never exists
	// with the process umask's mode.
	if f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600); err != nil {
		return nil, fmt.Errorf("mobilehost: create app database: %w", err)
	} else {
		f.Close()
	}
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(path))
	if err != nil {
		return nil, fmt.Errorf("mobilehost: open app database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("mobilehost: app database unreachable: %w", err)
	}
	return db, nil
}

// loadOrMint reads a 32-hex-character token from path, minting one with
// crypto/rand (0600, exclusive create) on first run. valid, when non-nil,
// re-checks a value read back: a token that is a security principal must
// match the grammar it was minted with, never be guessed.
func loadOrMint(path string, valid func(string) bool) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		v := strings.TrimSpace(string(b))
		if v == "" || (valid != nil && !valid(v)) {
			return "", fmt.Errorf("mobilehost: %s is corrupt; restore it or delete it to mint a new one", path)
		}
		return v, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("mobilehost: read %s: %w", path, err)
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("mobilehost: mint token: %w", err)
	}
	v := hex.EncodeToString(raw[:])
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return loadOrMint(path, valid)
		}
		return "", fmt.Errorf("mobilehost: write %s: %w", path, err)
	}
	if _, err := f.WriteString(v); err != nil {
		f.Close()
		return "", fmt.Errorf("mobilehost: write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("mobilehost: write %s: %w", path, err)
	}
	return v, nil
}

func validHexID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
