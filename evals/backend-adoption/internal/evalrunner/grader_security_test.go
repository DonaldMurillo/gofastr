package evalrunner

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The candidate server launched by startServer is unsupervised agent-written
// code running unsandboxed on the developer's machine. The ui-quality runner
// treats the identical artifact as untrusted (candidateEnvironment: strict
// name allowlist plus an isolated HOME), so credentials must not reach this
// launch either. The probe binary echoes its inherited environment on
// /healthz — exactly what a real candidate could exfiltrate back to its
// author over the grading connection.
func TestCandidateServerEnvIsAllowlisted(t *testing.T) {
	probeDir := filepath.Join(t.TempDir(), "envprobe")
	if err := os.MkdirAll(probeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	probeMain := `package main

import (
	"net/http"
	"os"
	"strings"
)

func main() {
	env := strings.Join(os.Environ(), "\n")
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(env))
	})
	_ = http.ListenAndServe(os.Getenv("PORT"), nil)
}
`
	files := map[string]string{
		"go.mod":  "module eval.test/envprobe\n\ngo 1.24.2\n",
		"main.go": probeMain,
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(probeDir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	buildCtx, buildCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer buildCancel()
	bin := filepath.Join(probeDir, executableName("envprobe"))
	if out, err := commandOutput(buildCtx, probeDir, "go", "build", "-o", bin, "."); err != nil {
		t.Fatalf("build env probe: %v\n%s", err, out)
	}

	// Five distinct credential shapes: cloud-provider prefix, SCM token,
	// bare TOKEN suffix, API_TOKEN suffix, and the connection-string class
	// this package's own denylist (looksCredentialBearing) already strips
	// for its codex child.
	sentinels := []string{
		"AWS_SECRET_ACCESS_KEY",
		"GITHUB_TOKEN",
		"HF_TOKEN",
		"CLOUDFLARE_API_TOKEN",
		"DATABASE_URL",
	}
	for _, name := range sentinels {
		t.Setenv(name, "gf-eval-sentinel")
	}

	serverCtx, serverCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer serverCancel()
	server, err := startServer(serverCtx, bin, probeDir,
		filepath.Join(probeDir, "probe.db"), filepath.Join(probeDir, "server.log"))
	if err != nil {
		t.Fatalf("start probe server: %v", err)
	}
	defer server.stop()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(server.baseURL + "/healthz")
	if err != nil {
		t.Fatalf("probe /healthz: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read probe /healthz: %v", err)
	}
	inherited := string(body)

	for _, name := range sentinels {
		if strings.Contains(inherited, name+"=") {
			t.Errorf("candidate server inherited %s; unsupervised agent-built code must get an allowlisted environment, not the developer's (cf. ui-quality candidateEnvironment)", name)
		}
	}
	if !strings.Contains(inherited, "PATH=") {
		t.Errorf("candidate server lost PATH; an allowlist must keep the tool vars the ui-quality twin keeps")
	}
}
