package desktop

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
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
	if v := os.Getenv(dataDirEnv); v != "" {
		if !filepath.IsAbs(v) {
			return "", fmt.Errorf("desktop: %s must be absolute, got %q", dataDirEnv, v)
		}
		base = v
	}
	return filepath.Join(base, id), nil
}

// appOptionsDataDir resolves the effective data directory for an app
// id, honouring an explicit override (Config.DataDir).
func appOptionsDataDir(override, id string) (string, error) {
	if override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("desktop: Config.DataDir %q must be absolute", override)
		}
		if err := fileperm.MkdirPrivate(override); err != nil {
			return "", fmt.Errorf("desktop: create data dir %s: %w", override, err)
		}
		return override, nil
	}
	return DataDir(id)
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
