package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

const dataDirEnv = "GOFASTR_SERVICE_DATA_DIR"

const devSuffix = ".dev"

type layout struct {
	dir string
}

func (l layout) socketPath() string { return filepath.Join(l.dir, "control.sock") }
func (l layout) secretPath() string { return filepath.Join(l.dir, "secret") }
func (l layout) logDir() string     { return filepath.Join(l.dir, "logs") }
func (l layout) logPath() string    { return filepath.Join(l.logDir(), "service.log") }
func (l layout) loginDir() string   { return filepath.Join(l.dir, "login") }

func baseDir() (string, error) {
	if v := os.Getenv(dataDirEnv); v != "" {
		if !filepath.IsAbs(v) {
			return "", fmt.Errorf("service: %s must be absolute, got %q", dataDirEnv, v)
		}
		if strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return "", fmt.Errorf("service: %s must not contain control characters, got %q", dataDirEnv, v)
		}
		return v, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("service: resolve user config dir: %w", err)
	}
	return base, nil
}

func (b *Battery) base() (string, error) {
	if b.baseOverride != "" {
		return b.baseOverride, nil
	}
	return baseDir()
}

func (b *Battery) layoutFor(dev bool) (layout, error) {
	base, err := b.base()
	if err != nil {
		return layout{}, err
	}
	name := b.cfg.ID
	if dev {
		name += devSuffix
		if id := devIsolationID(); id != "" {
			name += "-" + id
		}
	}
	return layout{dir: filepath.Join(base, name)}, nil
}

var devIsolationID = isolationID

func isolationID() string {
	rt, err := isolation.Resolve(".")
	if err != nil || !rt.Active() {
		return ""
	}
	return pathSegment(rt.ID())
}

func pathSegment(id string) string {
	plain := id != "" && len(id) <= 32
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			plain = false
		}
	}
	if plain {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])[:10]
}

func (l layout) ensure() error {
	if err := ensurePrivateDir(l.dir); err != nil {
		return err
	}
	return nil
}

func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("service: create %s: %w", dir, err)
	}
	//gofastr:allow(tightenshared) callers pass app-namespaced service dirs this package owns
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("service: restrict %s: %w", dir, err)
	}
	if err := fileperm.Restrict(dir, true); err != nil {
		return fmt.Errorf("service: restrict %s: %w", dir, err)
	}
	return nil
}

func (b *Battery) binDir() (string, error) {
	if currentOS == "windows" {
		local := os.Getenv("LOCALAPPDATA")
		if local == "" || !filepath.IsAbs(local) {
			return "", fmt.Errorf("service: %%LOCALAPPDATA%% is unset or relative (%q)", local)
		}
		return filepath.Join(local, "Programs", b.cfg.ID), nil
	}
	l, err := b.layoutFor(false)
	if err != nil {
		return "", err
	}
	return filepath.Join(l.dir, "bin"), nil
}

func (b *Battery) exeName() string {
	if currentOS == "windows" {
		return b.cfg.Name + ".exe"
	}
	return b.cfg.Name
}

func (b *Battery) installedExe() (string, error) {
	dir, err := b.binDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, b.exeName()), nil
}

func loadOrMintSecret(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		if len(b) < 32 {
			return "", fmt.Errorf("service: secret file %s is shorter than 32 bytes; run `service rotate-secret`", path)
		}
		return string(b), nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("service: read secret file: %w", err)
	}
	secret, err := randomToken()
	if err != nil {
		return "", fmt.Errorf("service: mint secret: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			if b, rerr := os.ReadFile(path); rerr == nil && len(b) >= 32 {
				return string(b), nil
			}
		}
		return "", fmt.Errorf("service: write secret file: %w", err)
	}
	if err := fileperm.RestrictOpenFile(f); err != nil {
		f.Close()
		return "", fmt.Errorf("service: restrict secret file: %w", err)
	}
	if _, err := f.Write([]byte(secret)); err != nil {
		f.Close()
		return "", fmt.Errorf("service: write secret file: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("service: write secret file: %w", err)
	}
	if err := fileperm.Restrict(path, false); err != nil {
		return "", fmt.Errorf("service: restrict secret file: %w", err)
	}
	return secret, nil
}

func rotateSecret(path string) error {
	secret, err := randomToken()
	if err != nil {
		return fmt.Errorf("service: mint secret: %w", err)
	}
	if err := fileperm.WriteOwnerOnly(path, []byte(secret)); err != nil {
		return fmt.Errorf("service: write secret file: %w", err)
	}
	return fileperm.Restrict(path, false)
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
