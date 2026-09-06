package desktop

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	// AppOptions opens app.db under the "sqlite3" driver name; the blank
	// import registers it so a main.go needs no import of its own.
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// dataDirEnv, when set, replaces os.UserConfigDir() as the base the
// per-app data dir lives under. Tests point it at a temp dir so a suite
// never touches the developer's real Application Support; CI does the
// same. The value must be absolute.
const dataDirEnv = "GOFASTR_DESKTOP_DATA_DIR"

// DataDir returns the per-app data directory under the OS user config
// dir (os.UserConfigDir → Application Support, %AppData%,
// $XDG_CONFIG_HOME), creating it 0700. id is the reverse-DNS app id.
// GOFASTR_DESKTOP_DATA_DIR overrides the base directory.
func DataDir(id string) (string, error) {
	if err := validateAppID(id); err != nil {
		return "", err
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("desktop: resolve user config dir: %w", err)
	}
	if v := os.Getenv(dataDirEnv); v != "" {
		if !filepath.IsAbs(v) {
			return "", fmt.Errorf("desktop: %s must be absolute, got %q", dataDirEnv, v)
		}
		base = v
	}
	dir := filepath.Join(base, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("desktop: create data dir %s: %w", dir, err)
	}
	return dir, nil
}

// appOptionsDataDir resolves the effective data directory for an app
// id, honouring an explicit override (Config.DataDir).
func appOptionsDataDir(override, id string) (string, error) {
	if override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("desktop: Config.DataDir %q must be absolute", override)
		}
		if err := os.MkdirAll(override, 0o700); err != nil {
			return "", fmt.Errorf("desktop: create data dir %s: %w", override, err)
		}
		return override, nil
	}
	return DataDir(id)
}

// secretFileName is the file under the data dir holding the app-wide
// secret handed to framework.WithSecret, so sessions survive restarts.
const secretFileName = "secret"

// loadOrMintSecret returns the stored secret, minting (32 bytes of
// crypto/rand, base64url) and persisting 0600 on first run. The
// create-with-O_EXCL + re-read dance makes two processes racing the
// first boot converge on ONE secret instead of each minting one.
func loadOrMintSecret(dir string) (string, error) {
	path := filepath.Join(dir, secretFileName)
	if b, err := os.ReadFile(path); err == nil {
		if s := string(b); s != "" {
			return s, nil
		}
		return "", fmt.Errorf("desktop: secret file %s is empty", path)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("desktop: read secret file: %w", err)
	}
	secret, err := randomToken()
	if err != nil {
		return "", fmt.Errorf("desktop: mint secret: %w", err)
	}
	if err := writeFileExclusive(path, []byte(secret)); err != nil {
		if os.IsExist(err) {
			// Lost the race: another process minted it first.
			b, rerr := os.ReadFile(path)
			if rerr == nil && len(b) > 0 {
				return string(b), nil
			}
		}
		return "", fmt.Errorf("desktop: write secret file: %w", err)
	}
	return secret, nil
}

// writeFileExclusive creates path (0600) and writes b, refusing to
// touch an existing file.
func writeFileExclusive(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// randomToken returns 32 bytes from crypto/rand, base64url-encoded.
// Every credential this package mints (boot token, session value,
// secret, script nonce) comes through here; a wedged CSPRNG is an
// error, never a fallback to something weaker.
func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// openAppDB opens (creating if needed) the SQLite database under dir
// through the sqlite/stdlib driver this package imports.
func openAppDB(dir string) (*sql.DB, error) {
	path := filepath.ToSlash(filepath.Join(dir, "app.db"))
	db, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		return nil, fmt.Errorf("desktop: open app database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("desktop: app database unreachable: %w", err)
	}
	return db, nil
}
