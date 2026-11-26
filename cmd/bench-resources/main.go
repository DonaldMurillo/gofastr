// bench-resources prints a Markdown table of binary size, peak RAM during
// go build, and idle and loaded RAM for the bench apps under
// benchmarks/apps/ plus cmd/gofastr and cmd/kiln. Run it with
// `make bench-resources`; -apps picks a subset, -load the request count,
// -out where binaries go. Unknown or empty -apps names are refused before
// any benchmark runs.
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type appSpec struct {
	Name    string
	Package string // Go import path or relative dir
	Port    int    // chosen so multiple apps can coexist if ever needed
	Probe   string // optional health URL; if empty we just dial the port
}

var apps = []appSpec{
	{Name: "minimal", Package: "./benchmarks/apps/minimal", Port: 18080, Probe: "/plaintext"},
	{Name: "crud", Package: "./benchmarks/apps/crud", Port: 18081, Probe: "/healthz"},
	{Name: "full", Package: "./benchmarks/apps/full", Port: 18082, Probe: "/healthz"},
	{Name: "cmd-gofastr", Package: "./cmd/gofastr", Port: 0}, // no server, build-only
	{Name: "cmd-kiln", Package: "./cmd/kiln", Port: 0},       // build-only (serve subcommand exists but skip to keep this short)
}

var probeClient = &http.Client{Timeout: 1 * time.Second}

type result struct {
	App          string
	BinSizeBytes int64
	BuildPeakRSS int64 // bytes
	BuildWall    time.Duration
	IdleRSS      int64 // bytes; 0 if not run
	LoadedRSS    int64 // bytes; 0 if not run
	LoadDuration time.Duration
	LoadRequests int
	BuildErr     string
	RuntimeErr   string
}

func main() {
	var (
		appsFlag = flag.String("apps", "", "comma-separated subset of app names to run (default: all)")
		load     = flag.Int("load", 200, "number of HTTP requests to issue against each running app for loaded-RAM measurement")
		outDir   = flag.String("out", "dist/bench/resources", "directory to write binaries to (also captures size)")
	)
	flag.Parse()

	abs, err := filepath.Abs(*outDir)
	if err != nil {
		fail("resolve %s: %v", *outDir, err)
	}
	*outDir = abs
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fail("mkdir %s: %v", *outDir, err)
	}

	selection := apps
	if *appsFlag != "" {
		selection = nil
		want := map[string]bool{}
		for n := range strings.SplitSeq(*appsFlag, ",") {
			want[strings.TrimSpace(n)] = true
		}
		for _, a := range apps {
			if want[a.Name] {
				selection = append(selection, a)
				delete(want, a.Name)
			}
		}
		if len(want) > 0 {
			unknown := make([]string, 0, len(want))
			for name := range want {
				unknown = append(unknown, name)
			}
			sort.Strings(unknown)
			fail("unknown app names: %q", unknown)
		}
	}

	var results []result
	for _, app := range selection {
		fmt.Fprintf(os.Stderr, "▶ %s …\n", app.Name)
		r := runOne(app, *outDir, *load)
		results = append(results, r)
	}

	printReport(results)
}

func runOne(app appSpec, outDir string, loadReqs int) result {
	r := result{App: app.Name}

	binPath := filepath.Join(outDir, app.Name)

	cmd := exec.Command("go", "build", "-o", binPath, app.Package)
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(outDir, ".gocache"))
	_ = os.RemoveAll(filepath.Join(outDir, ".gocache"))

	start := time.Now()
	if out, err := cmd.CombinedOutput(); err != nil {
		r.BuildErr = fmt.Sprintf("build failed: %v: %s", err, strings.TrimSpace(string(out)))
		return r
	}
	r.BuildWall = time.Since(start)
	r.BuildPeakRSS = peakRSSFromState(cmd.ProcessState)

	if fi, err := statUnder(outDir, app.Name); err {
		r.BinSizeBytes = fi
	}

	if app.Port == 0 {
		return r
	}
	runtimeRAM(&r, binPath, app, loadReqs)
	return r
}

func statUnder(dir, name string) (int64, bool) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return 0, false
	}
	defer root.Close()
	fi, err := root.Stat(name)
	if err != nil {
		return 0, false
	}
	return fi.Size(), true
}

func runtimeRAM(r *result, binPath string, app appSpec, loadReqs int) {
	cmd := exec.Command(binPath)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", app.Port))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		r.RuntimeErr = fmt.Sprintf("start: %v", err)
		return
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	probeURL := fmt.Sprintf("http://127.0.0.1:%d%s", app.Port, app.Probe)
	if !waitReady(probeURL, 5*time.Second) {
		r.RuntimeErr = "did not become ready in 5s"
		return
	}

	time.Sleep(500 * time.Millisecond)
	r.IdleRSS = readRSS(cmd.Process.Pid)

	loadStart := time.Now()
	for i := range loadReqs {
		resp, err := probeClient.Get(probeURL)
		if err != nil {
			r.RuntimeErr = fmt.Sprintf("load req %d: %v", i, err)
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	r.LoadDuration = time.Since(loadStart)
	r.LoadRequests = loadReqs

	time.Sleep(500 * time.Millisecond)
	r.LoadedRSS = readRSS(cmd.Process.Pid)
}

func readRSS(pid int) int64 {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	field := strings.TrimSpace(string(out))
	if field == "" {
		return 0
	}
	kb, err := strconv.ParseInt(field, 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024
}

func waitReady(url string, deadline time.Duration) bool {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		resp, err := probeClient.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func printReport(results []result) {
	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i].App, results[j].App
		ai := strings.HasPrefix(a, "cmd-")
		bi := strings.HasPrefix(b, "cmd-")
		if ai != bi {
			return !ai
		}
		return a < b
	})

	fmt.Println("# bench-resources report")
	fmt.Println()
	fmt.Printf("Platform: %s/%s, %d CPU, Go %s\n", runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.Version())
	fmt.Println()
	fmt.Println("| App         | Bin size | Build wall | Build peak RAM | Idle RAM | Loaded RAM | Load reqs/dur |")
	fmt.Println("|-------------|---------:|-----------:|---------------:|---------:|-----------:|--------------:|")
	for _, r := range results {
		if r.BuildErr != "" {
			fmt.Printf("| %-11s | — | — | — | — | — | %s |\n", r.App, escape(r.BuildErr))
			continue
		}
		idle := "n/a"
		loaded := "n/a"
		loadCol := "—"
		if r.IdleRSS > 0 {
			idle = fmtBytes(r.IdleRSS)
		}
		if r.LoadedRSS > 0 {
			loaded = fmtBytes(r.LoadedRSS)
		}
		if r.LoadRequests > 0 {
			loadCol = fmt.Sprintf("%d / %s", r.LoadRequests, r.LoadDuration.Round(time.Millisecond))
		}
		if r.RuntimeErr != "" {
			idle = escape(r.RuntimeErr)
		}
		fmt.Printf("| %-11s | %s | %s | %s | %s | %s | %s |\n",
			r.App,
			fmtBytes(r.BinSizeBytes),
			r.BuildWall.Round(10*time.Millisecond),
			fmtBytes(r.BuildPeakRSS),
			idle,
			loaded,
			loadCol,
		)
	}
	fmt.Println()
	fmt.Println("Notes:")
	fmt.Println("- Bin size: stripped of debug symbols only if you run `go build -ldflags=-s -w` separately; this runner uses default flags.")
	fmt.Println("- Build peak RAM: maximum resident set size of the `go build` subprocess (via Rusage.Maxrss).")
	fmt.Println("- Idle/Loaded RAM: RSS of the running binary via `ps -o rss=`, after a 500ms settle.")
	fmt.Println("- cmd-* targets are build-only; runtime columns are n/a.")
}

func fmtBytes(b int64) string {
	const (
		KB = 1 << 10
		MB = 1 << 20
		GB = 1 << 30
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/GB)
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/MB)
	case b >= KB:
		return fmt.Sprintf("%.0f KB", float64(b)/KB)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func escape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", " ")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
