package desktop

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

const dataDirEnv = "GOFASTR_DESKTOP_DATA_DIR"

// DataDir returns the per-app data directory under os.UserConfigDir, creating it 0700.
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

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
