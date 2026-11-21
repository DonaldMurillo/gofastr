package evalrunner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCandidateEnvironmentDropsCredentialsAndIsolatesHome(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "secret")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	t.Setenv("APPDATA", "C:\\Users\\real\\AppData\\Roaming")
	t.Setenv("LOCALAPPDATA", "C:\\Users\\real\\AppData\\Local")
	t.Setenv("GOENV", "C:\\Users\\real\\go\\env")
	t.Setenv("PATH", os.Getenv("PATH"))
	home := filepath.Join(t.TempDir(), "home")
	env := candidateEnvironment(home, "GOWORK=off")
	joined := "\n" + strings.Join(env, "\n") + "\n"
	for _, forbidden := range []string{"\nOPENAI_API_KEY=", "\nAWS_SECRET_ACCESS_KEY="} {
		if strings.Contains(strings.ToUpper(joined), forbidden) {
			t.Fatalf("candidate environment leaked credential %s: %v", forbidden, env)
		}
	}
	if strings.Contains(joined, `C:\Users\real`) || strings.Contains(joined, "GOENV=") {
		t.Fatalf("candidate environment leaked the parent home/config paths: %v", env)
	}
	for _, want := range []string{"HOME=" + home, "USERPROFILE=" + home, "GOWORK=off"} {
		if !strings.Contains(joined, "\n"+want+"\n") {
			t.Fatalf("candidate environment missing %q: %v", want, env)
		}
	}
}

func TestCandidateEnvironmentProvidesUsableIsolatedTempToSubprocess(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := prepareCandidateHome(home); err != nil {
		t.Fatal(err)
	}
	env := candidateEnvironment(home, "GOFASTR_TEMP_HELPER=1")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCandidateEnvironmentTempHelper$")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("temp-using candidate subprocess failed: %v\n%s", err, out)
	}
	wantRoot := filepath.Join(home, "tmp")
	if !strings.Contains(string(out), wantRoot) {
		t.Fatalf("candidate subprocess used a temp path outside %q:\n%s", wantRoot, out)
	}
	for _, dir := range []string{filepath.Join(home, "AppData", "Roaming"), filepath.Join(home, "AppData", "Local")} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("isolated profile directory %q is unavailable: %v", dir, err)
		}
	}
}

func TestCandidateEnvironmentTempHelper(t *testing.T) {
	if os.Getenv("GOFASTR_TEMP_HELPER") != "1" {
		return
	}
	temp, err := os.CreateTemp("", "gofastr-candidate-")
	if err != nil {
		t.Fatal(err)
	}
	name := temp.Name()
	if err := temp.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	fmt.Println(name)
}

// The isolated HOME must not silently relocate Go's module cache: go
// resolves GOMODCACHE from $HOME/go/pkg/mod when unset, so pointing HOME at
// an empty directory would force every candidate gate to re-download the
// dependency graph `go mod tidy` just resolved into the real cache (and fail
// outright offline).
func TestCandidateEnvKeepsWarmModuleCache(t *testing.T) {
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Skipf("go env GOMODCACHE unavailable: %v", err)
	}
	want := strings.TrimSpace(string(out))
	if want == "" {
		t.Skip("empty GOMODCACHE")
	}
	env := candidateEnvironment(filepath.Join(t.TempDir(), "home"))
	joined := "\n" + strings.Join(env, "\n") + "\n"
	if !strings.Contains(joined, "\nGOMODCACHE="+want+"\n") {
		t.Fatalf("candidate environment must pin the runner's module cache %q: %v", want, env)
	}
}
