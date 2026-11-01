// Command host is the processmodule-demo's host app. It registers the demo
// module (examples/processmodule-demo) as an UNTRUSTED process module, so
// the child runs under this host's sandbox backend (bwrap on Linux,
// sandbox-exec on macOS), and serves the module's routes:
//
//	go build -o dist/examples/processmodule-demo ./examples/processmodule-demo
//	go run ./examples/processmodule-demo/host -module dist/examples/processmodule-demo
//	curl localhost:8091/demo/hello
//
// Before it registers the module the host asks
// [framework.App.ProcessModuleSandbox] whether this machine can confine an
// untrusted module at all. That call probes the backend with the P1–P7
// conformance suite, re-running THIS binary as the probe child; the
// framework's package init answers the probe, so main has no hook for it.
// On a host that does not conform the module stays off and the host says
// which probe failed; -trust=trusted runs it with crash isolation only.
//
// -check-sandbox prints the conformance report as JSON and exits. The
// tests (and the sandbox-conformance CI job) use it to run the probe
// through a real host binary rather than the framework's test binary.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/examples/processmodule-demo/surface"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// moduleName is the demo module's operator-approved name.
const moduleName = "demo"

// exitNoSandbox is the exit code when an untrusted module cannot run here.
const exitNoSandbox = 3

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("host", flag.ContinueOnError)
	fs.SetOutput(stderr)
	modulePath := fs.String("module", os.Getenv("DEMO_MODULE"), "path to the built processmodule-demo binary (or DEMO_MODULE)")
	addr := fs.String("addr", ":8091", "listen address")
	dbPath := fs.String("db", "", "SQLite file for the module store (default: a temporary file)")
	trust := fs.String("trust", "untrusted", "trust tier: untrusted (sandboxed) or trusted (crash isolation only)")
	check := fs.Bool("check-sandbox", false, "probe this host's sandbox, print the report as JSON, and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	db, cleanup, err := openDB(*dbPath)
	if err != nil {
		fmt.Fprintln(stderr, "host:", err)
		return 1
	}
	defer cleanup()

	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "processmodule-demo-host"}),
	)

	if *check {
		return checkSandbox(app, stdout, stderr)
	}

	var tier framework.TrustTier
	switch *trust {
	case "untrusted":
		tier = framework.TrustUntrusted
	case "trusted":
		tier = framework.TrustTrusted
	default:
		fmt.Fprintf(stderr, "host: -trust must be untrusted or trusted, got %q\n", *trust)
		return 2
	}
	if *modulePath == "" {
		fmt.Fprintln(stderr, "host: -module is required: build the module with `go build -o dist/examples/processmodule-demo ./examples/processmodule-demo`")
		return 2
	}
	desc, err := descriptor(*modulePath, tier)
	if err != nil {
		fmt.Fprintln(stderr, "host:", err)
		return 1
	}

	// Ask first, so a machine without a conforming sandbox still boots
	// and says why. RegisterProcessModule would refuse the untrusted
	// module anyway (it panics with the same report); there is no
	// fallback to the trusted runner.
	if tier == framework.TrustUntrusted {
		if _, err := app.ProcessModuleSandbox(); err != nil {
			fmt.Fprintf(stderr, "host: this machine cannot run untrusted modules, so the demo module stays off.\n%v\n"+
				"Run with -trust=trusted to see the module with crash isolation only.\n", err)
			return exitNoSandbox
		}
	}

	// The demo module requests no grants, so the approved set is empty.
	app.RegisterProcessModule(desc, framework.ApprovedGrants{})
	// Process modules install disabled; the operator enables them. This
	// demo is its own operator.
	if err := app.ProcessModules().Enable(context.Background(), moduleName); err != nil {
		fmt.Fprintln(stderr, "host: enable:", err)
		return 1
	}

	fmt.Fprintf(stdout, "host: serving the %s demo module on %s (GET /demo/hello)\n", tier, *addr)
	if err := app.Start(*addr); err != nil {
		fmt.Fprintln(stderr, "host:", err)
		return 1
	}
	return 0
}

// descriptor builds the operator-approved descriptor for the module at
// path. A real deploy pins ArtifactSHA256 to the build it reviewed; the
// demo hashes whatever binary it is pointed at, so the operator's
// approval here is choosing the -module path.
func descriptor(path string, tier framework.TrustTier) (framework.ProcessModuleDescriptor, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return framework.ProcessModuleDescriptor{}, err
	}
	sum, err := sha256File(abs)
	if err != nil {
		return framework.ProcessModuleDescriptor{}, fmt.Errorf("module %s: %w", abs, err)
	}
	d := framework.ProcessModuleDescriptor{
		Name:           moduleName,
		Version:        "1.0.0",
		ArtifactPath:   abs,
		ArtifactSHA256: sum,
		TrustTier:      tier,
		Routes: []framework.RouteDeclaration{
			{ID: "hello", Method: "GET", Path: "/demo/hello"},
			{ID: "tree", Method: "GET", Path: "/demo/tree"},
			// The hello screen's button names this route as its
			// action_ref. The host renders an action only to a route the
			// descriptor declares, so leaving it out fails the whole
			// screen closed (a 503), by design.
			{ID: "refresh", Method: "POST", Path: "/demo/refresh"},
		},
		Tools: []framework.ToolDigest{
			{ID: "ping", SHA256: framework.ModuleToolDigest(surface.PingTool())},
		},
	}
	d.SurfaceSHA256, err = framework.ComputeSurfaceSHA256(d)
	return d, err
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// openDB opens the SQLite store the supervisor keeps module state in.
func openDB(path string) (*sql.DB, func(), error) {
	cleanup := func() {}
	if path == "" {
		dir, err := os.MkdirTemp("", "processmodule-demo-host-*")
		if err != nil {
			return nil, nil, err
		}
		cleanup = func() { _ = os.RemoveAll(dir) }
		path = filepath.Join(dir, "host.db")
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return db, func() { _ = db.Close(); cleanup() }, nil
}

// sandboxReport is the JSON -check-sandbox prints.
type sandboxReport struct {
	Backend   string        `json:"backend"`
	Available bool          `json:"available"`
	Missing   string        `json:"missing,omitempty"`
	Conforms  bool          `json:"conforms"`
	Probes    []probeReport `json:"probes"`
}

type probeReport struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Reported bool   `json:"reported"`
	Detail   string `json:"detail,omitempty"`
}

// checkSandbox runs the app's sandbox probe and prints the report. It
// exits 0 whenever a report was produced, conforming or not: the JSON
// carries the verdict.
func checkSandbox(app *framework.App, stdout, stderr io.Writer) int {
	var report framework.ConformanceReport
	runner, err := app.ProcessModuleSandbox()
	var cerr *framework.SandboxConformanceError
	switch {
	case err == nil:
		report = runner.Report()
	case errors.As(err, &cerr):
		report = cerr.Report
	default:
		fmt.Fprintln(stderr, "host:", err)
		return 1
	}
	out := sandboxReport{
		Backend:   report.Backend,
		Available: report.Available,
		Missing:   report.MissingReason,
		Conforms:  report.Conforms(),
	}
	for _, res := range report.Results {
		out.Probes = append(out.Probes, probeReport{
			ID:       res.ID.String(),
			Title:    res.ID.Title(),
			Status:   res.Status.String(),
			Reported: res.Reported,
			Detail:   res.Detail,
		})
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(stderr, "host:", err)
		return 1
	}
	return 0
}
