package desktop

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// DataDirEnv names the absolute base-directory override for DataDir.
const DataDirEnv = "GOFASTR_DESKTOP_DATA_DIR"

// DataDir returns the per-app data directory under the OS user config
// dir (os.UserConfigDir → Application Support, %AppData%,
// $XDG_CONFIG_HOME), creating it 0700. id is the reverse-DNS app id.
// GOFASTR_DESKTOP_DATA_DIR overrides the base directory.
func DataDir(id string) (string, error) {
	dir, err := dataDirPath(id)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("desktop: create data dir %s: %w", dir, err)
	}
	if err := fileperm.Restrict(dir, true); err != nil {
		return "", fmt.Errorf("desktop: restrict data dir: %w", err)
	}
	return dir, nil
}

// dataDirPath resolves without touching the filesystem. Persistent browser
// profiles validate the path before creating it, rather than following a link
// or changing permissions on a pre-existing directory.
func dataDirPath(id string) (string, error) {
	if err := validateAppID(id); err != nil {
		return "", err
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("desktop: resolve user config dir: %w", err)
	}
	if v := os.Getenv(DataDirEnv); v != "" {
		if !filepath.IsAbs(v) {
			return "", fmt.Errorf("desktop: %s must be absolute, got %q", DataDirEnv, v)
		}
		base = v
	}
	return filepath.Join(base, id), nil
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
