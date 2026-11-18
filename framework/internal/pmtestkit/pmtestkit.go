// Package pmtestkit holds process-module test helpers for framework tests.
package pmtestkit

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// User is the session user AuthMiddleware resolves from Cookie "sid=<id>".
type User struct{ ID string }

// GetID returns the user's id.
func (u *User) GetID() string { return u.ID }

// InstallOwnerExtractor maps a *User in the context to its owner id until t ends.
func InstallOwnerExtractor(t testing.TB) {
	t.Helper()
	prev := owner.GetExtractor()
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		raw, ok := handler.GetUser(ctx)
		if !ok || raw == nil {
			return nil, false
		}
		if u, ok := raw.(*User); ok {
			return u.GetID(), true
		}
		return nil, false
	})
	t.Cleanup(func() { owner.SetExtractor(prev) })
}

// AuthMiddleware installs policy, and the user and roles of a "sid=<id>"
// session cookie, the way battery/auth does.
func AuthMiddleware(policy *access.RolePolicy, rolesFor func(string) []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if policy != nil {
				ctx = access.WithPolicy(ctx, policy)
			}
			if c := r.Header.Get("Cookie"); strings.HasPrefix(c, "sid=") {
				sid := strings.TrimPrefix(c, "sid=")
				ctx = handler.SetUser(ctx, &User{ID: sid})
				if rolesFor != nil {
					if roles := rolesFor(sid); len(roles) > 0 {
						ctx = access.WithRoles(ctx, roles)
					}
				}
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SHA256File returns the lowercase hex SHA-256 of the file at path.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// ExecutablePath adds the .exe suffix Windows needs to run a built file.
func ExecutablePath(path string) string {
	if runtime.GOOS == "windows" && filepath.Ext(path) == "" {
		return path + ".exe"
	}
	return path
}

// ChildArtifact copies the test binary, to run as a module child, and returns (path, sha256).
func ChildArtifact(t testing.TB) (string, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	dst := filepath.Join(t.TempDir(), ExecutablePath("child"))
	in, err := os.Open(exe)
	if err != nil {
		t.Fatalf("open exe: %v", err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create dst: %v", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy: %v", err)
	}
	out.Close()
	if err := os.Chmod(dst, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	sha, err := SHA256File(dst)
	if err != nil {
		t.Fatalf("sha256: %v", err)
	}
	return dst, sha
}

var (
	demoBinOnce sync.Once
	demoBinPath string
	demoBinErr  error
)

// DemoBinary builds internal/testapps/processmodule-demo once per test
// binary and returns (path, sha256).
func DemoBinary(t testing.TB) (string, string) {
	t.Helper()
	demoBinOnce.Do(func() {
		gomod, err := exec.Command("go", "env", "GOMOD").Output()
		if err != nil || len(strings.TrimSpace(string(gomod))) == 0 {
			demoBinErr = fmt.Errorf("go env GOMOD: %v", err)
			return
		}
		repoRoot := filepath.Dir(strings.TrimSpace(string(gomod)))
		dir, err := os.MkdirTemp("", "gofastr-demobuild-*")
		if err != nil {
			demoBinErr = fmt.Errorf("mkdtemp: %w", err)
			return
		}
		bin := filepath.Join(dir, ExecutablePath("demo"))
		cmd := exec.Command("go", "build", "-o", bin, "./internal/testapps/processmodule-demo")
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			demoBinErr = fmt.Errorf("go build demo: %v\n%s", err, out)
			return
		}
		demoBinPath = bin
	})
	if demoBinErr != nil {
		t.Fatalf("build demo binary: %v", demoBinErr)
	}
	sha, err := SHA256File(demoBinPath)
	if err != nil {
		t.Fatalf("sha256 demo: %v", err)
	}
	return demoBinPath, sha
}
