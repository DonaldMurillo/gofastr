package apps_test

//gofastr:test-reads benchmarks/apps/minimal/ benchmarks/apps/crud/ benchmarks/apps/full/

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	// The apps' direct imports, so cmd/affected selects this test when a
	// dependency the built binaries link changes (TestBlankImportsCoverApps).
	_ "github.com/DonaldMurillo/gofastr/battery/search"
	_ "github.com/DonaldMurillo/gofastr/battery/storage"
	_ "github.com/DonaldMurillo/gofastr/core-ui/app"
	_ "github.com/DonaldMurillo/gofastr/core-ui/component"
	_ "github.com/DonaldMurillo/gofastr/core/render"
	_ "github.com/DonaldMurillo/gofastr/core/router"
	_ "github.com/DonaldMurillo/gofastr/core/schema"
	_ "github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/framework/uihost"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Every bench app boots and answers GET /healthz: cmd/bench-resources waits
// on it, so an app that dies at Start (a duplicate /healthz) bricks the lane.
func TestBootHealthzReachable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binaries")
	}
	for _, app := range []string{"minimal", "crud", "full"} {
		t.Run(app, func(t *testing.T) {
			t.Parallel()
			bootAndProbeHealthz(t, app)
		})
	}
}

func bootAndProbeHealthz(t *testing.T, app string) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "app")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, "./"+app)
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("setup broken: build: %v", err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup broken: free port: %v", err)
	}
	port := fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
	l.Close()

	var stderr bytes.Buffer
	srv := exec.Command(bin)
	srv.Dir = dir
	srv.Env = append(os.Environ(), "PORT="+port)
	srv.Stdout = io.Discard
	srv.Stderr = &stderr
	if err := srv.Start(); err != nil {
		t.Fatalf("setup broken: start: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- srv.Wait() }()
	t.Cleanup(func() { _ = srv.Process.Kill(); <-exited })

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if resp, err := client.Get("http://127.0.0.1:" + port + "/healthz"); err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case werr := <-exited:
			exited <- werr
			t.Fatalf("SECURITY: [bench-healthz-boot] %s bench app exited before /healthz answered (wait=%v); stderr tail: %.400s", app, werr, stderr.String())
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("SECURITY: [bench-healthz-boot] %s bench app never answered GET /healthz within 10s; stderr tail: %.400s", app, stderr.String())
		}
	}
}

func TestBlankImportsCoverApps(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "boot_test.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("setup broken: parse: %v", err)
	}
	have := map[string]bool{}
	for _, im := range f.Imports {
		if im.Name != nil && im.Name.Name == "_" {
			p, _ := strconv.Unquote(im.Path.Value)
			have[p] = true
		}
	}
	for _, app := range []string{"minimal", "crud", "full"} {
		out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, "./"+app).Output()
		if err != nil {
			t.Fatalf("setup broken: go list %s: %v", app, err)
		}
		for _, imp := range strings.Fields(string(out)) {
			if strings.HasPrefix(imp, "github.com/DonaldMurillo/gofastr/") && !have[imp] {
				t.Errorf("%s imports %s; add it to boot_test.go's blank imports", app, imp)
			}
		}
	}
}
