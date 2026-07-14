package evalrunner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

func runCommand(ctx context.Context, dir, logPath string, env []string, program string, args ...string) error {
	cmd := exec.CommandContext(ctx, program, args...)
	configureCommandCancellation(cmd)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append([]string(nil), env...)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := runOwnedCommand(cmd)
	if logPath != "" {
		if mkErr := os.MkdirAll(filepath.Dir(logPath), 0o755); mkErr != nil {
			return mkErr
		}
		if writeErr := os.WriteFile(logPath, out.Bytes(), 0o644); writeErr != nil && err == nil {
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

var candidateEnvironmentAllowlist = map[string]bool{
	"APPDATA": true, "CC": true, "CGO_ENABLED": true, "COMSPEC": true,
	"CXX": true, "GOARCH": true, "GOCACHE": true,
	"GOEXPERIMENT": true, "GOMODCACHE": true, "GOOS": true, "GOPATH": true,
	"GOROOT": true, "HOME": true, "HOMEDRIVE": true, "HOMEPATH": true,
	"LANG": true, "LANGUAGE": true, "LC_ALL": true, "LOCALAPPDATA": true,
	"LOGNAME": true, "NUMBER_OF_PROCESSORS": true, "OS": true, "PATH": true,
	"PATHEXT": true, "PKG_CONFIG_PATH": true, "PROCESSOR_ARCHITECTURE": true,
	"PROCESSOR_IDENTIFIER": true, "PROGRAMDATA": true, "PROGRAMFILES": true,
	"PROGRAMFILES(X86)": true, "PROGRAMW6432": true, "SHELL": true,
	"SSL_CERT_DIR": true, "SSL_CERT_FILE": true, "SYSTEMDRIVE": true,
	"SYSTEMROOT": true, "TEMP": true, "TMP": true, "TMPDIR": true,
	"TZ": true, "USER": true, "USERPROFILE": true, "WINDIR": true,
}

func candidateEnvironment(home string, overrides ...string) []string {
	var allowed []string
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && candidateEnvironmentAllowlist[strings.ToUpper(name)] {
			allowed = append(allowed, entry)
		}
	}
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
	// re-download the dependency graph `go mod tidy` just resolved into the
	// real cache — and failing outright without network access. Pin the
	// runner's resolved cache; the module cache is safe to share.
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
// own (non-isolated) environment — the same resolution the warm-up
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

func startOwnedCommand(cmd *exec.Cmd) (func(), error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	cleanup, err := attachCommandProcessTree(cmd.Process)
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, fmt.Errorf("attach process tree for pid %d: %w", cmd.Process.Pid, err)
	}
	return cleanup, nil
}

func runOwnedCommand(cmd *exec.Cmd) error {
	cleanup, err := startOwnedCommand(cmd)
	if err != nil {
		return err
	}
	err = cmd.Wait()
	cleanup()
	return err
}

func executableName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func waitForHealth(ctx context.Context, url string) error {
	return waitForOwnedHealth(ctx, url, nil, nil)
}

func waitForOwnedHealth(ctx context.Context, url string, exited <-chan struct{}, exitErr func() error) error {
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var last string
	var consecutiveHealthy int
	for {
		if exited != nil {
			select {
			case <-exited:
				return fmt.Errorf("candidate process exited before stable health: %v", exitErr())
			default:
			}
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
		case <-exited:
			return fmt.Errorf("candidate process exited before stable health: %v", exitErr())
		case <-ctx.Done():
			return fmt.Errorf("health check %s failed: %s: %w", url, last, ctx.Err())
		case <-ticker.C:
		}
	}
}
