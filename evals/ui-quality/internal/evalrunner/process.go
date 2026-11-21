package evalrunner

import (
	"github.com/DonaldMurillo/gofastr/internal/fileperm"

	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/evals/internal/childenv"
	"github.com/DonaldMurillo/gofastr/evals/internal/proctree"
)

func runCommand(ctx context.Context, dir, logPath string, env []string, program string, args ...string) error {
	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append([]string(nil), env...)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := proctree.Run(ctx, cmd)
	if logPath != "" {
		if mkErr := os.MkdirAll(filepath.Dir(logPath), 0o700); mkErr != nil {
			return mkErr
		}
		if writeErr := fileperm.WriteOwnerOnly(logPath, out.Bytes()); writeErr != nil && err == nil {
			return writeErr
		}
	}
	if err != nil {
		return fmt.Errorf("%s %v: %w", program, args, err)
	}
	return nil
}

func inheritedEnvironment(overrides ...string) []string {
	return environmentWithOverrides(os.Environ(), overrides...)
}

func candidateEnvironment(home string, overrides ...string) []string {
	// The allowlist lives in evals/internal/childenv so the
	// backend-adoption runner uses the same one; it used to be here
	// only, and that runner handed its candidate os.Environ() whole.
	allowed := childenv.Allowlisted()
	homeOverrides := []string{
		"HOME=" + home,
		"USERPROFILE=" + home,
		"APPDATA=" + filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA=" + filepath.Join(home, "AppData", "Local"),
		"TEMP=" + filepath.Join(home, "tmp"),
		"TMP=" + filepath.Join(home, "tmp"),
		"TMPDIR=" + filepath.Join(home, "tmp"),
	}
	// go derives GOMODCACHE from $HOME/go/pkg/mod when unset. The isolated
	// home would relocate it to an empty directory, forcing every gate to
	// re-download the dependency graph `go mod tidy` just resolved, and
	// failing outright without network access. Pin the runner's resolved
	// cache; the module cache is safe to share.
	if modCache := resolvedGoModCache(); modCache != "" {
		homeOverrides = append(homeOverrides, "GOMODCACHE="+modCache)
	}
	if volume := filepath.VolumeName(home); volume != "" {
		homeOverrides = append(homeOverrides, "HOMEDRIVE="+volume, "HOMEPATH="+strings.TrimPrefix(home, volume))
	}
	return environmentWithOverrides(allowed, append(homeOverrides, overrides...)...)
}

var (
	goModCacheOnce sync.Once
	goModCachePath string
)

// resolvedGoModCache resolves `go env GOMODCACHE` once, under the runner's
// own (non-isolated) environment, the same resolution the warm-up
// `go mod tidy` used.
func resolvedGoModCache() string {
	goModCacheOnce.Do(func() {
		out, err := exec.Command("go", "env", "GOMODCACHE").Output()
		if err != nil {
			return
		}
		goModCachePath = strings.TrimSpace(string(out))
	})
	return goModCachePath
}

func prepareCandidateHome(home string) error {
	for _, dir := range []string{
		filepath.Join(home, "tmp"),
		filepath.Join(home, "AppData", "Roaming"),
		filepath.Join(home, "AppData", "Local"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func environmentWithOverrides(base []string, overrides ...string) []string {
	values := make(map[string]string, len(base)+len(overrides))
	for _, entry := range append(append([]string(nil), base...), overrides...) {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			continue
		}
		values[strings.ToUpper(name)] = entry
	}
	env := make([]string, 0, len(values))
	for _, entry := range values {
		env = append(env, entry)
	}
	sort.Strings(env)
	return env
}

func waitForOwnedHealth(ctx context.Context, url string, command *proctree.Command) error {
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var last string
	var consecutiveHealthy int
	for {
		select {
		case <-command.Done():
			return fmt.Errorf("candidate process exited before stable health: %v", command.Wait())
		default:
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				consecutiveHealthy++
				if consecutiveHealthy >= 2 {
					return nil
				}
			} else {
				consecutiveHealthy = 0
			}
			last = resp.Status
		} else {
			consecutiveHealthy = 0
			last = err.Error()
		}
		select {
		case <-command.Done():
			return fmt.Errorf("candidate process exited before stable health: %v", command.Wait())
		case <-ctx.Done():
			return fmt.Errorf("health check %s failed: %s: %w", url, last, ctx.Err())
		case <-ticker.C:
		}
	}
}
