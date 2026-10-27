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

// dataDirEnv, when set, replaces os.UserConfigDir() as the base the
// state directories live under. Tests point it at a temp dir so a
// suite never touches the developer's real Application Support. The
// value must be absolute.
const dataDirEnv = "GOFASTR_SERVICE_DATA_DIR"

// devSuffix names the foreground dev run's state directory: <ID>.dev,
// a sibling of the installed one, so a dev run never shares a
// database, secret or control socket with an installed copy.
const devSuffix = ".dev"

// layout is the on-disk shape of one mode's state directory.
type layout struct {
	dir string
}

func (l layout) socketPath() string { return filepath.Join(l.dir, "control.sock") }
func (l layout) secretPath() string { return filepath.Join(l.dir, "secret") }
func (l layout) logDir() string     { return filepath.Join(l.dir, "logs") }
func (l layout) logPath() string    { return filepath.Join(l.logDir(), "service.log") }
func (l layout) loginDir() string   { return filepath.Join(l.dir, "login") }

// baseDir is the directory the per-app state directories live under:
// os.UserConfigDir (Application Support, %AppData%, $XDG_CONFIG_HOME),
// the same base battery/desktop uses, or GOFASTR_SERVICE_DATA_DIR.
func baseDir() (string, error) {
	if v := os.Getenv(dataDirEnv); v != "" {
		if !filepath.IsAbs(v) {
			return "", fmt.Errorf("service: %s must be absolute, got %q", dataDirEnv, v)
		}
		// The base is written into the service definition (a systemd
		// ExecStart word, a plist string, a task argument); a line break
		// there could add a directive the user never wrote.
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

// base is the state base this process uses: the --base-dir the
// service definition passes the daemon, else baseDir. install writes
// the CLI's resolved base into the definition, so a daemon started by
// launchd, systemd or Task Scheduler, none of which see the shell's
// environment, uses the same directory as the CLI that talks to it.
func (b *Battery) base() (string, error) {
	if b.baseOverride != "" {
		return b.baseOverride, nil
	}
	return baseDir()
}

// layoutFor resolves (without creating) the state directory of the
// installed daemon (dev false) or the foreground dev run (dev true).
// A dev run inside an isolated worktree gets its own directory,
// <ID>.dev-<isolation id>, so two worktrees' dev runs of one app keep
// separate databases and control sockets, as their ports already are.
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

// devIsolationID is isolationID, a variable so tests pin it.
var devIsolationID = isolationID

// isolationID is the worktree isolation id when isolation is active
// for the working directory, else "", as a path segment.
func isolationID() string {
	rt, err := isolation.Resolve(".")
	if err != nil || !rt.Active() {
		return ""
	}
	return pathSegment(rt.ID())
}

// pathSegment returns id when it is a short [A-Za-z0-9_-] word and a
// 10-hex-digit hash of it otherwise: GOFASTR_ISOLATION_ID is free text,
// and it becomes part of a directory name.
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

// ensure creates the state directory owner-only: 0700 on Unix (and
// tightened to 0700 if it already existed wider), an owner-only DACL
// on Windows, where mode bits mean nothing.
func (l layout) ensure() error {
	if err := ensurePrivateDir(l.dir); err != nil {
		return err
	}
	return nil
}

// ensurePrivateDir creates dir and every missing parent 0700 and
// tightens dir itself.
func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("service: create %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("service: restrict %s: %w", dir, err)
	}
	if err := fileperm.Restrict(dir, true); err != nil {
		return fmt.Errorf("service: restrict %s: %w", dir, err)
	}
	return nil
}

// binDir is where install copies the executable. Windows keeps
// programs out of the roaming profile, under %LOCALAPPDATA%\Programs;
// elsewhere it is bin/ inside the installed state directory.
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

// exeName is the installed executable's file name.
func (b *Battery) exeName() string {
	if currentOS == "windows" {
		return b.cfg.Name + ".exe"
	}
	return b.cfg.Name
}

// installedExe is the absolute path of the installed executable.
func (b *Battery) installedExe() (string, error) {
	dir, err := b.binDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, b.exeName()), nil
}

// loadOrMintSecret returns the stored secret, minting one (32 bytes of
// crypto/rand, base64url) and persisting it 0600 on first run. The
// O_EXCL create and re-read make two processes racing the first boot
// converge on one secret.
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

// rotateSecret replaces the stored secret with a fresh one.
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

// randomToken returns 32 bytes from crypto/rand, base64url-encoded.
// Every credential this package mints comes through here; a failing
// CSPRNG is an error, never a fallback.
func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
